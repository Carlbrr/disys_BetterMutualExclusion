package main

import (
	"DistributedMutualExclusion/proto"
	"bufio"
	"context"
	"flag"
	"log"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc"
)

type STATE int

var wait *sync.WaitGroup

func init() {
	wait = &sync.WaitGroup{}
}

type Node struct {
	name         string
	id           int
	state        STATE
	timestamp    int
	ports        []string
	replyCounter int
	queue        customQueue
	protoNode    proto.Node
	mutex        sync.Locker
	proto.UnimplementedExclusionServiceServer
}

const (
	RELEASED STATE = iota
	WANTED
	HELD
)

func (n *Node) ReceiveRequest(ctx context.Context, requestMessage *proto.RequestMessage) (*proto.Void, error) {
	log.Printf("%s received request from: %s", n.name, requestMessage.User.Name)
	log.Printf("----------------------------------------------------------")

	n.timestamp = n.updateClock(int(requestMessage.Timestamp))

	requestID := int(requestMessage.User.Id)
	void := proto.Void{}

	if n.shouldDefer(requestMessage) {
		log.Printf("%s is deferring request from: %s", n.name, requestMessage.User.Name)
		log.Printf("----------------------------------------------------------")
		n.queue.Enqueue(requestID)
		return &void, nil
	} else {
		log.Printf("%s is sending reply to: %s", n.name, requestMessage.User.Name)
		log.Printf("----------------------------------------------------------")
		n.SendReply(requestID)
		return &void, nil
	}
}

func (n *Node) shouldDefer(requestMessage *proto.RequestMessage) bool {
	if n.state == HELD {
		return true
	}

	if n.state != WANTED {
		return false
	}

	if n.timestamp < int(requestMessage.Timestamp) {
		return true
	}

	if n.timestamp == int(requestMessage.Timestamp) && n.id < int(requestMessage.User.Id) {
		return true
	}

	return false
}

func (n *Node) AccessCritical(ctx context.Context, requestMessage *proto.RequestMessage) (*proto.ReplyMessage, error) {
	log.Printf("%s, %d, Stamp: %d Requesting access to critical", n.name, n.id, n.timestamp)
	log.Printf("----------------------------------------------------------")
	n.state = WANTED
	n.MessageAll(ctx, requestMessage)
	reply := proto.ReplyMessage{Timestamp: int32(n.timestamp), User: &n.protoNode}

	return &reply, nil
}

func (n *Node) ReceiveReply(ctx context.Context, replyMessage *proto.ReplyMessage) (*proto.Void, error) {

	n.replyCounter++

	if n.replyCounter == len(n.ports)-1 {
		n.EnterCriticalSection()
	}

	n.updateClock(int(replyMessage.GetTimestamp()))

	return &proto.Void{}, nil
}

func (n *Node) SendReply(index int) {
	port := n.ports[index]
	conn, err := grpc.Dial(":"+port, grpc.WithInsecure())
	if err != nil {
		log.Fatalf("Failed to connect to port: %v", err)
	}
	defer conn.Close()
	client := proto.NewExclusionServiceClient(conn)
	client.ReceiveReply(context.Background(), &proto.ReplyMessage{})
}

func (n *Node) EnterCriticalSection() {
	n.state = HELD
	log.Printf("%s entered the critical section", n.name)
	log.Printf("----------------------------------------------------------")
	n.timestamp++
	time.Sleep(5 * time.Second)

	n.LeaveCriticalSection()
}

func (n *Node) LeaveCriticalSection() {
	log.Printf("%s is leaving the critical section", n.name)
	log.Printf("----------------------------------------------------------")
	n.state = RELEASED
	n.timestamp++
	n.replyCounter = 0

	for !n.queue.Empty() {
		index := n.queue.Front()
		n.queue.Dequeue()
		log.Printf("%s: reply to defered request", n.name)
		log.Printf("----------------------------------------------------------")
		n.SendReply(index)
	}
}

func (n *Node) MessageAll(ctx context.Context, msg *proto.RequestMessage) error {
	for i := 0; i < len(n.ports); i++ {
		if i != n.id {
			conn, err := grpc.Dial(":"+n.ports[i], grpc.WithInsecure())

			if err != nil {
				log.Fatalf("Failed to dial this port(Message all): %v", err)
			}
			defer conn.Close()
			client := proto.NewExclusionServiceClient(conn)

			client.ReceiveRequest(ctx, msg)
		}
	}
	return nil
}

func (n *Node) listen() {
	lis, err := net.Listen("tcp", ":"+n.ports[n.id])
	if err != nil {
		log.Fatalf("Failed to listen port: %v", err)
	}
	grpcServer := grpc.NewServer()

	proto.RegisterExclusionServiceServer(grpcServer, n)

	log.Printf("node listening at %v", lis.Addr())
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("Failed serve server: %v", err)
	}
}

func (n *Node) updateClock(otherTimestamp int) int {
	if n.timestamp < otherTimestamp {
		return otherTimestamp + 1
	} else {
		return n.timestamp
	}
}

func main() {
	name := flag.String("N", "Anonymous", "name")
	id := flag.Int("I", 0, "id")
	flag.Parse()

	queue := &customQueue{queue: make([]int, 0)}

	done := make(chan int)
	n := &Node{
		name:                                *name,
		id:                                  *id,
		state:                               RELEASED,
		timestamp:                           0,
		ports:                               []string{},
		replyCounter:                        0,
		queue:                               *queue,
		protoNode:                           proto.Node{Id: int32(*id), Name: *name},
		mutex:                               &sync.Mutex{},
		UnimplementedExclusionServiceServer: proto.UnimplementedExclusionServiceServer{},
	}

	file, err := os.Open("ports.txt")
	if err != nil {
		log.Fatal(err)
	}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		n.ports = append(n.ports, scanner.Text())
	}
	// OKAY HVORDAN GØR MAN DET HER
	wait.Add(1)
	//go listenToOtherPorts()

	go func() {
		defer wait.Done()

		inScanner := bufio.NewScanner(os.Stdin)
		for inScanner.Scan() {

			if strings.ToLower(inScanner.Text()) == "exit" {
				os.Exit(1)

			} else if strings.ToLower(inScanner.Text()) == "request access" {
				request := proto.RequestMessage{User: &n.protoNode, Timestamp: int32(n.timestamp)}
				reply, err := n.AccessCritical(context.Background(), &request)
				if err != nil {
					log.Fatalf("Failed to Request %v, %d", err, reply.Timestamp)
					break
				}
			} else {
				log.Printf("Invalid command")
				log.Printf("Use 'Exit' to terminate Node")
				log.Printf("Use 'request access' to request access to critical section")
				log.Printf("----------------------------------------------------------")
			}
		}
	}()
	go func() {
		wait.Wait()
		close(done)
	}()
	n.listen()
	<-done
}

type customQueue struct {
	queue []int
	lock  sync.RWMutex
}

func (c *customQueue) Enqueue(name int) {
	c.lock.Lock()
	defer c.lock.Unlock()
	c.queue = append(c.queue, name)
}

func (c *customQueue) Dequeue() {
	if len(c.queue) > 0 {
		c.lock.Lock()
		defer c.lock.Unlock()
		c.queue = c.queue[1:]
	}
}

func (c *customQueue) Front() int {
	if len(c.queue) > 0 {
		c.lock.Lock()
		defer c.lock.Unlock()
		return c.queue[0]
	}
	return -1
}

func (c *customQueue) Size() int {
	return len(c.queue)
}

func (c *customQueue) Empty() bool {
	return len(c.queue) == 0
}
