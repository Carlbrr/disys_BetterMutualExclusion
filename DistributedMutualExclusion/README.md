# DistributedMutualExclusion

To run the program:
- Create 4 different terminal windows
- Use the format: "go run Node/main.go -N (Process name) -I (Process Id). -N and -I being command line flags. N being arbitrary, while I has to be a unique integer 0 <= I <= 3
- Exactly 4 processes must run before continuing.
- Entering "exit" will terminate a process
- Entering "request access" will prompt the node to request access to the critical section.
