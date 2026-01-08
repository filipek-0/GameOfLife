package main

import (
	"flag"
	"math"
	"net"
	"net/rpc"
	"sync"
	"time"

	"uk.ac.bris.cs/gameoflife/stubs"
	"uk.ac.bris.cs/gameoflife/util"
)

var (
	workers  = make([]*rpc.Client, 0)
	workersM sync.Mutex

	world  [][]byte
	turns  int
	worldM sync.Mutex

	stopped  = false
	stoppedM sync.Mutex

	interrupts = make(chan bool)
	closes     = make(chan bool)
)

func calculateWorldSlices(threads, imageHeight int) ([][][]byte, []int) {
	worldSlices := make([][][]byte, threads)
	heights := make([]int, threads)
	div := float64(imageHeight) / float64(threads)
	for i := 0; i < threads; i++ {
		start := int(math.Round(div * float64(i)))
		end := int(math.Round(div * float64(i+1)))
		height := end - start + 2
		worldSlice := make([][]byte, height)

		startA := start - 1
		if startA < 0 {
			startA = imageHeight - 1
		}
		worldSlice[0] = world[startA]
		for j := 1; j < height-1; j++ {
			worldSlice[j] = world[start+j-1]
		}
		endA := end
		if endA > imageHeight-1 {
			endA = 0
		}
		worldSlice[height-1] = world[endA]
		worldSlices[i] = worldSlice
		heights[i] = height - 2
	}
	return worldSlices, heights
}

func calculateAliveCells() []util.Cell {
	// array to keep hold of alive cells
	var aliveCells []util.Cell
	for i, worldCell := range world {
		for j := range worldCell {
			// if the current cell is alive, add it to the array of alive cells
			if world[i][j] == 255 {
				aliveCells = append(aliveCells, util.Cell{X: j, Y: i}) // transposed because of weird test cases
			}
		}
	}
	return aliveCells
}

func countAliveCells() int {
	n := 0
	for i, worldCell := range world {
		for j := range worldCell {
			if world[i][j] == 255 {
				n++
			}
		}
	}
	return n
}

type Broker struct{}

func (b *Broker) Subscribe(req stubs.SubscribeRequest, _ *stubs.SubscribeResponse) (err error) {
	client, err := rpc.Dial("tcp", req.Address)
	if err != nil {
		return err
	}

	workersM.Lock()
	workers = append(workers, client)
	workersM.Unlock()
	return
}

func (b *Broker) PreBreak(_ stubs.PreBreakRequest, _ *stubs.PreBreakResponse) (err error) {
	stoppedM.Lock()
	stopped = false
	stoppedM.Unlock()
	return
}

func (b *Broker) BreakWorld(req stubs.BreakWorldRequest, res *stubs.BreakWorldResponse) (err error) {
	workersM.Lock()
	threads := len(workers)
	workersM.Unlock()
	if req.Threads < threads {
		threads = req.Threads
	}

	worldM.Lock()
	world = req.World
	turns = 0
	worldM.Unlock()

	worldSlices, heights := calculateWorldSlices(threads, req.ImageHeight)

	responses := make([]*stubs.RunWorldResponse, threads)
	done := make(chan *rpc.Call, threads)
out:
	for turns < req.Turns {
		select {
		case <-interrupts:
			break out
		default:
			for i := 0; i < threads; i++ {
				responses[i] = new(stubs.RunWorldResponse)
				request := stubs.RunWorldRequest{
					Width:      req.ImageWidth,
					Height:     heights[i],
					WorldSlice: worldSlices[i],
				}
				workers[i].Go(stubs.RunWorldHandler, request, responses[i], done)
			}

			for i := 0; i < threads; i++ {
				<-done
			}

			worldM.Lock()
			h := 0
			for i, response := range responses {
				for j := 0; j < heights[i]; j++ {
					for k := 0; k < req.ImageWidth; k++ {
						world[j+h][k] = response.WorldSlice[j][k]
					}
				}
				h += heights[i]
			}
			turns++
			worldM.Unlock()
		}
	}

	res.World = world
	res.CompletedTurns = turns
	res.AliveCells = calculateAliveCells()

	if turns == req.Turns {
		stoppedM.Lock()
		if stopped {
			stoppedM.Unlock()
			<-interrupts
		} else {
			stopped = true
			stoppedM.Unlock()
		}
	}
	return
}

func (b *Broker) CountAlive(_ *stubs.CountAliveRequest, response *stubs.CountAliveResponse) (err error) {
	worldM.Lock()
	response.CompletedTurns = turns
	response.CellsCount = countAliveCells()
	worldM.Unlock()
	return
}

func (b *Broker) CurrentState(_ *stubs.CurrentStateRequest, response *stubs.CurrentStateResponse) (err error) {
	worldM.Lock()
	response.CompletedTurns = turns
	response.World = world
	worldM.Unlock()
	return
}

func (b *Broker) Pause(_ *stubs.PauseRequest, _ *stubs.PauseResponse) (err error) {
	stoppedM.Lock()
	if !stopped {
		stopped = true
		stoppedM.Unlock()
		interrupts <- true
	} else {
		stoppedM.Unlock()
	}
	return
}

func (b *Broker) Close(_ *stubs.PauseRequest, _ *stubs.PauseResponse) (err error) {
	closes <- true
	return
}

func main() {
	pAddr := flag.String("port", "8030", "Port to listen on")
	flag.Parse()
	rpc.Register(&Broker{})
	listener, _ := net.Listen("tcp", ":"+*pAddr)
	go rpc.Accept(listener)

	<-closes
	req := stubs.CloseRequest{}
	res := new(stubs.CloseResponse)
	for _, worker := range workers {
		worker.Call(stubs.WorkerCloseHandler, req, res)
		worker.Close()
	}
	<-time.After(500 * time.Millisecond)
	listener.Close()
}
