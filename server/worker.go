package main

import (
	"flag"
	"net"
	"net/rpc"
	"sync"
	"time"

	"github.com/ChrisGora/semaphore"
	"uk.ac.bris.cs/gameoflife/stubs"
)

var (
	directions = [8][2]int{
		{-1, 0},  //up
		{1, 0},   //down
		{0, -1},  //left
		{0, 1},   //right
		{-1, -1}, //left-up
		{-1, 1},  //right-up
		{1, -1},  //left-down
		{1, 1},   //right-down
	}

	pAddr     *string
	broker    *rpc.Client
	preWorker *rpc.Client
	sucWorker *rpc.Client

	index int

	single     bool
	totalTurns int
	width      int
	height     int
	fullHeight int

	slices  = make(map[int][][]byte)
	turns   int
	slicesM sync.RWMutex

	haloTS semaphore.Semaphore
	haloBS semaphore.Semaphore
	halos  = make(chan bool)

	interrupt = make(chan bool)
	closes    = make(chan bool)
)

func calculateNextState(world [][]byte) [][]byte {
	// variable for the number of alive neighbours
	aliveNeighbours := 0

	// 2d array for the updated state as the world
	nextWorld := make([][]byte, height)
	// allocate inner arrays of nextWorld
	for i := 0; i < height; i++ {
		nextWorld[i] = make([]byte, width)
	}

	// we have to visit every element of the 2D array and compute the number of neighbours alive
	for i := 0; i < height; i++ {
		for j := 0; j < width; j++ {
			// reset neighbours for each cell
			aliveNeighbours = 0

			// check each neighbour by going over every possible direction of a neighbour
			// modulo operation used to include wrapping, if i=0 and direction[0]=-1, then
			// (-1 + 16) % 16 = 15 which is the correctly wrapped value
			for _, direction := range directions {
				//ni := (i + direction[0] + p.ImageWidth) % p.ImageWidth
				ni := i + direction[0]
				//if ni < 0 {
				//	ni += height
				//} else if ni >= height {
				//	ni -= height
				//}

				//nj := (j + direction[1] + p.ImageHeight) % p.ImageHeight
				nj := j + direction[1]
				if nj < 0 {
					nj += width
				} else if nj >= width {
					nj -= width
				}

				//fmt.Printf("ni: %d, nj: %d\n", ni, nj)

				// check whether the neighbour is alive, if yes, increment the counter
				if world[ni+1][nj] == 255 {
					aliveNeighbours++
				}
			}

			// change current cell according to the neighbouring cells
			if aliveNeighbours < 2 || aliveNeighbours > 3 {
				nextWorld[i][j] = 0
			} else if aliveNeighbours == 3 {
				nextWorld[i][j] = 255
			} else {
				nextWorld[i][j] = world[i+1][j]
			}
		}
	}
	return nextWorld
}

func innerSlice(turn int) [][]byte {
	return slices[turn][1 : fullHeight-1]
}

func haloListener() {
out:
	for {
		select {
		case <-halos:
			break out
		case halos <- true:
			haloTS.Wait()
			haloBS.Wait()
		}
	}
}

type GOLOperations struct{}

func (g *GOLOperations) Init(req *stubs.WorkerInitRequest, _ *stubs.WorkerInitResponse) (err error) {
	//fmt.Println("Worker executing Init")
	single = req.AddrPre == *pAddr
	if !single {
		preWorker, err = rpc.Dial("tcp", req.AddrPre)
		if err != nil {
			panic(err)
		}

		if req.AddrPre == req.AddrSuc {
			sucWorker = preWorker
		} else {
			sucWorker, err = rpc.Dial("tcp", req.AddrSuc)
			if err != nil {
				panic(err)
			}
		}
	}

	totalTurns = req.Turns
	width = req.Width
	height = req.Height
	fullHeight = height + 2

	haloTS = semaphore.Init(3, 0)
	haloBS = semaphore.Init(3, 0)

	go haloListener()

	slices[1] = make([][]byte, fullHeight)

	//fmt.Println("Worker finishing Init")
	return
}

func (g *GOLOperations) RunWorld(req *stubs.RunWorldRequest, res *stubs.RunWorldResponse) (err error) {
	//fmt.Println("Worker executing RunWorld")
	slicesM.Lock()
	slices[0] = req.WorldSlice
	turns = 0
	slicesM.Unlock()

	completedTurns := 0

out:
	for turns < totalTurns {
		select {
		case stop := <-interrupt:
			if stop || <-interrupt {
				break out
			}
		case <-halos:
			//fmt.Println("RunWorld on turns ", turns)
			nextSlice := calculateNextState(slices[turns])
			slicesM.Lock()
			copy(innerSlice(turns+1), nextSlice)
			slices[turns+2] = make([][]byte, fullHeight)
			turns++
			slicesM.Unlock()

			response := new(stubs.UpdateTurnsResponse)
			request := stubs.UpdateTurnsRequest{
				Index: index,
				Turns: turns,
			}
			broker.Call(stubs.UpdateTurnsHandler, request, response)

			slicesM.Lock()
			for i := completedTurns; i < response.CompletedTurns; i++ {
				delete(slices, i)
			}
			slicesM.Unlock()
			completedTurns = response.CompletedTurns

			if turns < totalTurns {
				if single {
					slicesM.Lock()
					slices[turns][0] = nextSlice[height-1]
					slices[turns][fullHeight-1] = nextSlice[0]
					haloTS.Post()
					haloBS.Post()
					slicesM.Unlock()
				} else {
					responseS := new(stubs.SetHaloInTurnsResponse)
					requestS := stubs.SetHaloInTurnsRequest{
						Turns: turns,
						Halo:  nextSlice[0],
						IsTop: false,
					}
					preWorker.Call(stubs.SetHaloInTurnsHandler, requestS, responseS)

					requestS = stubs.SetHaloInTurnsRequest{
						Turns: turns,
						Halo:  nextSlice[height-1],
						IsTop: true,
					}
					sucWorker.Call(stubs.SetHaloInTurnsHandler, requestS, responseS)
				}
			}

		}
	}

	res.CompletedTurns = turns

	haloTS.Post()
	haloBS.Post()
	halos <- true
	//fmt.Println("Worker finishing RunWorld")
	return
}

func (g *GOLOperations) CountAliveInTurns(req *stubs.CountAliveInTurnsRequest, res *stubs.CountAliveInTurnsResponse) (err error) {
	//fmt.Println("Worker executing CountAliveInTurns ", req.Turns)
	slicesM.RLock()
	n := 0
	inner := innerSlice(req.Turns)
	for i := 0; i < height; i++ {
		for j := 0; j < width; j++ {
			if inner[i][j] == 255 {
				n++
			}
		}
	}
	res.CellsCount = n
	slicesM.RUnlock()
	//fmt.Println("Worker finishing CountAliveInTurns ", req.Turns)
	return
}

func (g *GOLOperations) SliceInTurns(req *stubs.SliceInTurnsRequest, res *stubs.SliceInTurnsResponse) (err error) {
	slicesM.RLock()
	res.Slice = innerSlice(req.Turns)
	slicesM.RUnlock()
	return
}

func (g *GOLOperations) SetHaloInTurns(req *stubs.SetHaloInTurnsRequest, _ *stubs.SetHaloInTurnsResponse) (err error) {
	slicesM.Lock()
	slice := slices[req.Turns]
	if req.IsTop {
		slice[0] = req.Halo
		haloTS.Post()
	} else {
		slice[fullHeight-1] = req.Halo
		haloBS.Post()
	}
	slicesM.Unlock()
	return
}

func (g *GOLOperations) Pause(_ *stubs.PauseRequest, res *stubs.PauseResponse) (err error) {
	//fmt.Println("Worker executing Pause")
	interrupt <- false
	res.CompletedTurns = turns
	//fmt.Println("Worker finishing Pause")
	return
}

func (g *GOLOperations) Stop(_ *stubs.StopRequest, _ *stubs.StopResponse) (err error) {
	//fmt.Println("Worker executing Stop")
	interrupt <- true
	//fmt.Println("Worker finishing Stop")
	return
}

func (g *GOLOperations) Close(_ *stubs.CloseRequest, _ *stubs.CloseResponse) (err error) {
	//fmt.Println("Worker executing Close")
	defer func() {
		closes <- true
	}()
	//fmt.Println("Worker finishing Close")
	return
}

func main() {
	pAddr = flag.String("port", "8040", "Port to listen on")
	pIp := flag.String("ip", "127.0.0.1", "Ip of worker")
	pBroker := flag.String("broker", "127.0.0.1:8030", "IP:port string to connect to as broker")
	flag.Parse()

	rpc.Register(&GOLOperations{})
	listener, _ := net.Listen("tcp", ":"+*pAddr)
	go rpc.Accept(listener)

	broker, _ = rpc.Dial("tcp", *pBroker)
	req := stubs.SubscribeRequest{Address: *pIp + ":" + *pAddr}
	res := new(stubs.SubscribeResponse)
	broker.Call(stubs.SubscribeHandler, req, res)
	index = res.Index

	<-closes
	broker.Close()
	<-time.After(500 * time.Millisecond)
	listener.Close()
}
