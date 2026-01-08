# Game Of Life

## Overview
This project implements parallel and distributed versions of Conway’s Game of Life in Go. It explores scalability, communication overhead, and fault tolerance in distributed systems, as well as concurrent programming using goroutines and synchronization on a single machine. All findings are evaluated using benchmarking and CPU profiling.

## Features
- Parallel computation using goroutines over a partitioned game board to calculate the next state
- Mutex-based protection of critical sections, synchronisation through channels, and event-based visualisation
- Distributed execution via worker-based RPC calls
- Publisher-subscriber architecture with a broker to decouple clients from workers and support easier scalability
- Fault tolerance through worker failure detection and dynamic reassignment of tasks
- Halo exchange reducing communication overhead by exchanging only boundary slices between workers illustrating the communication tradeoff
- Deployment-ready on cloud VMs (e.g. AWS EC2), with a configurable number of nodes
- Support for multiple concurrent clients

  <img width="439.5" height="230" alt="image" src="https://github.com/user-attachments/assets/8ee0cbf6-d39d-4655-b43f-47271d57839d" />

  Architecture of the pub-sub broker-based distributed _Game of Life_ system.

## Results and Observations
- The parallel implementation shows diminishing returns beyond a certain number of workers as the overheads incurred by goroutine scheduling dominates
- Replacing channel-based communication with memory sharing via custom data structures greatly improves runtime by eliminating communication overhead from channels
- Replacing modular arithmetic with conditional indexing substantially reduces runtime
- Increasing the number of servers in the distributed system initially decreases total runtime, with the most effective configuration being a broker with 11 workers, beyond this point, communication overhead outweights the faster computation
- Halo exchange improves performance, especially for large game boards, by avoiding repeated sending of full board slices on every turn

Detailed architectural diagrams, benchmarking, CPU profiling, and experimental analysis are provided in the accompanying [report](/report.pdf).

## Versions
This project was developed in multiple versions, each focusing on a specific feature or extension to facilitate experimentation and comparison of different approaches. The most notable versions are preserved as tagged releases:

**v1.0-distributed**  
Initial distributed implementation using a worker-based RPC architecture with a broker.

**v1.1-distributed-fault**  
Introduces fault tolerance, allowing the simulation to continue under partial failure.

**v1.2-distributed-halo**  
Extends the distributed system to reduce communication overhead by exchanging only boundary slices between workers.

**v2.0-parallel**  
Optimized parallel implementation on a single machine.

Full source snapshots are available as tagged releases.

## How to run

**Requirements**
- Go 1.7 or newer

**Run (distributed, 4 local workers)**
- Open multiple terminals:
```bash
# broker terminal:
cd broker
go run .

# worker terminals
cd server
go run . -port=8040
go run . -port=8050
go run . -port=8060
go run . -port=8070

# client terminal
go run .
```

**Run (v2.0-parallel)**
```bash
go run .
```

**Tests**
- The project includes automated tests:
```bash
# main Game of Life tests
go test ./tests -v -run TestGoL

# key presses (navigation) tests
go test ./tests -v -run TestKeyboard -sdl
```

**Cloud**
- The distributed system was tested on multiple AWS EC2 instances, running the broker and workers on separate nodes.

## Acknowledgements
This project originated from a university coursework and was significantly extended beyond the standard concurrent and distributed architecture requirements.

A provided SDL-based visualization skeleton was used as a starting point and extended to support the parallel and distributed implementations.


University of Bristol - Computer Systems A coursework

Conway's _Game of Life_

