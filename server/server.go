package main

import (
	"fmt"
	"math"
	"net/rpc"
	"sync"

	"uk.ac.bris.cs/gameoflife/stubs"
	"uk.ac.bris.cs/gameoflife/util"
)

var DIRECTIONS = [8][2]int{
	{-1, 0},  //up
	{1, 0},   //down
	{0, -1},  //left
	{0, 1},   //right
	{-1, -1}, //left-up
	{-1, 1},  //right-up
	{1, -1},  //left-down
	{1, 1},   //right-down
}

func calculateNextState(width, height int, world [][]byte) [][]byte {
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
			for _, direction := range DIRECTIONS {
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

func calculateWorldSlices(threads, imageHeight int, world [][]byte) ([][][]byte, []int) {
	worldSlices := make([][][]byte, threads)
	heights := make([]int, threads)
	div := float64(imageHeight) / float64(threads)
	for i := 0; i < threads; i++ {
		start := int(math.Round(div * float64(i)))
		end := int(math.Round(div * float64(i+1)))
		fmt.Printf("Worker %d takes row %d to %d\n", i, start, end-1)
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

func calculateAliveCells(world [][]byte) []util.Cell {
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

func countAliveCells(world [][]byte) int {
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

type GOLService struct {
	//IsBroker bool

	Addrs    []string
	Clients  []*rpc.Client
	ClientsM sync.Mutex

	World  [][]byte
	Turns  int
	WorldM sync.Mutex

	Interrupts chan bool
	Finished   chan bool
	Closes     chan bool
}

func (s *GOLService) Subscribe(req *stubs.SubscribeRequest, _ *stubs.SubscribeResponse) (err error) {
	fmt.Println("Server executing Subscribe")
	client, err := rpc.Dial("tcp", req.Address)
	if err != nil {
		return err
	}

	s.ClientsM.Lock()
	s.Addrs = append(s.Addrs, req.Address)
	s.Clients = append(s.Clients, client)
	fmt.Printf("Currently have %d clients\n", len(s.Clients))
	s.ClientsM.Unlock()
	fmt.Println("Server finishing Subscribe")
	return
}

func (s *GOLService) Addresses(_ *stubs.AddressesRequest, res *stubs.AddressesResponse) (err error) {
	fmt.Println("Server executing Addresses")
	res.Addresses = s.Addrs
	fmt.Println("Server finishing Addresses")
	return
}

func (s *GOLService) BreakWorld(req *stubs.BreakWorldRequest, res *stubs.BreakWorldResponse) (err error) {
	fmt.Printf("Broker executing BreakWorld with ImageHeight %d and Threads %d and Turns %d\n",
		req.ImageHeight, req.Threads, req.Turns)
	s.ClientsM.Lock()
	threads := len(s.Clients)
	s.ClientsM.Unlock()
	if req.Threads < threads {
		threads = req.Threads
	}

	s.WorldM.Lock()
	s.World = req.World
	s.Turns = 0
	s.WorldM.Unlock()

	var worldSlices [][][]byte
	var heights []int
	var responses []*stubs.RunWorldResponse
	var dones []chan *rpc.Call

	prevThreads := -1

out:
	for s.Turns < req.Turns {
		select {
		case <-s.Interrupts:
			break out
		default:
			if threads != prevThreads {
				if threads > 0 {
					worldSlices, heights = calculateWorldSlices(threads, req.ImageHeight, s.World)
					responses = make([]*stubs.RunWorldResponse, threads)
					dones = make([]chan *rpc.Call, threads)
					for i := 0; i < threads; i++ {
						dones[i] = make(chan *rpc.Call, 1)
					}
				} else {
					worldSlices = make([][][]byte, 1)
					worldSlices[0] = make([][]byte, req.ImageHeight+2)
					worldSlices[0][0] = s.World[req.ImageHeight-1]
					for i := 0; i < req.ImageHeight; i++ {
						worldSlices[0][i+1] = s.World[i]
					}
					worldSlices[0][req.ImageHeight+1] = s.World[0]
					heights = make([]int, 1)
					heights[0] = req.ImageHeight
					responses = make([]*stubs.RunWorldResponse, 1)
				}
				prevThreads = threads
			}

			if threads > 0 {
				s.ClientsM.Lock()
				for i := 0; i < threads; i++ {
					responses[i] = new(stubs.RunWorldResponse)
					request := stubs.RunWorldRequest{
						Width:      req.ImageWidth,
						Height:     heights[i],
						WorldSlice: worldSlices[i],
					}
					s.Clients[i].Go(stubs.RunWorldHandler, request, responses[i], dones[i])
				}

				var handleDones func(int, []int)
				handleDones = func(n int, mapper []int) {
					errIs := make([]int, 0)

					for i := 0; i < n; i++ {
						call := <-dones[i]
						if call.Error != nil {
							fmt.Println(call.Error.Error())
							s.Clients = append(s.Clients[:i], s.Clients[i+1:]...)
							errIs = append(errIs, i)
						}
					}

					if len(errIs) == 0 {
						return
					}

					if len(s.Clients) < threads {
						threads = len(s.Clients)
					}

					var i, j int
					for i = 0; i < len(errIs); {
						newMapper := make([]int, 0)
						for j = 0; i < len(errIs) && (j < threads || threads <= 0); i, j = i+1, j+1 {
							origin := mapper[errIs[i]]
							newMapper = append(newMapper, origin)
							responses[origin] = new(stubs.RunWorldResponse)
							request := stubs.RunWorldRequest{
								Width:      req.ImageWidth,
								Height:     heights[origin],
								WorldSlice: worldSlices[origin],
							}
							if threads > 0 {
								s.Clients[j].Go(stubs.RunWorldHandler, request, responses[origin], dones[i])
							} else {
								s.RunWorld(&request, responses[origin])
							}
						}
						if threads > 0 {
							handleDones(j, newMapper)
						}
					}
					return
				}

				mapper := make([]int, threads)
				for i := 0; i < threads; i++ {
					mapper[i] = i
				}
				handleDones(threads, mapper)
				s.ClientsM.Unlock()
			} else {
				responses[0] = new(stubs.RunWorldResponse)
				request := stubs.RunWorldRequest{
					Width:      req.ImageWidth,
					Height:     req.ImageHeight,
					WorldSlice: worldSlices[0],
				}
				s.RunWorld(&request, responses[0])
			}

			s.WorldM.Lock()
			h := 0
			for i := 0; i < len(responses); i++ {
				for j := 0; j < heights[i]; j++ {
					for k := 0; k < req.ImageWidth; k++ {
						s.World[j+h][k] = responses[i].WorldSlice[j][k]
					}
				}
				h += heights[i]
			}
			s.Turns++
			s.WorldM.Unlock()
		}
	}

	res.World = s.World
	res.CompletedTurns = s.Turns
	res.AliveCells = calculateAliveCells(s.World)
	fmt.Println("Broker finishing BreakWorld")

	select {
	case s.Finished <- true:
	default:
	}
	return
}

func (s *GOLService) CurrentState(_ *stubs.CurrentStateRequest, response *stubs.CurrentStateResponse) (err error) {
	fmt.Println("Broker executing CurrentState")
	s.WorldM.Lock()
	response.CompletedTurns = s.Turns
	response.World = s.World
	response.CellsCount = countAliveCells(s.World)
	s.WorldM.Unlock()
	fmt.Println("Broker finishing CurrentState")
	return
}

func (s *GOLService) Pause(_ *stubs.PauseRequest, _ *stubs.PauseResponse) (err error) {
	fmt.Println("Broker executing Pause")
	s.Interrupts <- true
	<-s.Finished
	fmt.Println("Broker finishing Pause")
	return
}

func (s *GOLService) Close(req *stubs.CloseRequest, _ *stubs.CloseResponse) (err error) {
	fmt.Println("Server executing Close")
	s.Closes <- req.IsBroker
	fmt.Println("Server finishing Close")
	return
}

func (s *GOLService) RunWorld(request *stubs.RunWorldRequest, response *stubs.RunWorldResponse) (err error) {
	//fmt.Println("Worker executing RunWorld")
	width := request.Width
	height := request.Height

	nextWorld := calculateNextState(width, height, request.WorldSlice)

	response.WorldSlice = nextWorld

	//fmt.Println("Worker finishing RunWorld")
	return
}
