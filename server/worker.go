package main

import (
	"flag"
	"net"
	"net/rpc"
	"time"

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

	closes = make(chan bool)
)

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

			for _, direction := range directions {
				ni := i + direction[0]
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

type GOLOperations struct{}

func (g *GOLOperations) RunWorld(request *stubs.RunWorldRequest, response *stubs.RunWorldResponse) (err error) {
	width := request.Width
	height := request.Height

	nextWorld := calculateNextState(width, height, request.WorldSlice)

	response.WorldSlice = nextWorld

	return
}

func (g *GOLOperations) Close(_ *stubs.CloseRequest, _ *stubs.CloseResponse) (err error) {
	closes <- true
	return
}

func getIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		panic(err)
	}

	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				return ipnet.IP.String()
			}
		}
	}

	return "127.0.0.1"
}

func main() {
	pAddr := flag.String("port", "8040", "Port to listen on")
	pLocal := flag.Bool("local", true, "running on local machine")
	pBroker := flag.String("broker", "127.0.0.1:8030", "IP:port string to connect to as broker")
	flag.Parse()

	rpc.Register(&GOLOperations{})
	listener, _ := net.Listen("tcp", ":"+*pAddr)
	go rpc.Accept(listener)

	client, _ := rpc.Dial("tcp", *pBroker)
	ip := "127.0.0.1"
	if !*pLocal {
		ip = getIP()
	}
	req := stubs.SubscribeRequest{Address: ip + ":" + *pAddr}
	res := new(stubs.SubscribeResponse)
	client.Call(stubs.SubscribeHandler, req, res)
	client.Close()

	<-closes
	<-time.After(500 * time.Millisecond)
	listener.Close()
}
