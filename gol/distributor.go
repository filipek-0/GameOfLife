package gol

import (
	"flag"
	"fmt"
	"net/rpc"
	"strings"
	"sync"
	"time"

	"uk.ac.bris.cs/gameoflife/stubs"
)

var (
	inited = false
	paused = false

	pBroker string
	client  *rpc.Client

	p Params
	c distributorChannels

	addresses           = make(chan string)
	tickerTriggers      = make(chan bool)
	keyListenerTriggers = make(chan bool)
	pauseKeyPresses     = make(chan rune)

	turnsToRun int

	lastBackup  int64
	backupWorld [][]byte
	backupTurns int
	backupAlive int
	backupM     sync.Mutex
)

type distributorChannels struct {
	events     chan<- Event
	keyPresses <-chan rune
	ioCommand  chan<- ioCommand
	ioIdle     <-chan bool
	ioFilename chan<- string
	ioOutput   chan<- uint8
	ioInput    <-chan uint8
}

func getCurrentStateAndBackup() ([][]byte, int, int) {
	if time.Now().UnixMilli()-lastBackup > 2 {
		request := stubs.CurrentStateRequest{}
		response := new(stubs.CurrentStateResponse)
		err := client.Call(stubs.CurrentStateHandler, request, response)
		if err != nil {
			panic(err)
		}
		backupM.Lock()
		lastBackup = time.Now().UnixMilli()
		backupWorld = response.World
		backupTurns = p.Turns - turnsToRun + response.CompletedTurns
		backupAlive = response.CellsCount
		backupM.Unlock()
	}
	return backupWorld, backupTurns, backupAlive
}

// ticker report the number of cells that are still alive every 2 seconds when gol is running
func ticker(seconds time.Duration) {
	for {
		<-tickerTriggers
	out:
		for {
			select {
			case <-time.After(seconds * time.Second):
				_, turn, count := getCurrentStateAndBackup()
				c.events <- AliveCellsCount{turn, count}
			case <-tickerTriggers:
				break out
			}
		}
	}
}

func keyListener() {
	for {
		<-keyListenerTriggers
	out:
		for {
			select {
			case key := <-c.keyPresses:
				if paused {
					pauseKeyPresses <- key
					if <-keyListenerTriggers {
						break out
					}
					break
				}

				switch key {
				case 's':
					world, turn, _ := getCurrentStateAndBackup()
					outFile := fmt.Sprintf("%dx%dx%d", p.ImageWidth, p.ImageHeight, turn)
					c.ioCommand <- ioOutput
					c.ioFilename <- outFile

					for i := range world {
						for j := range world[i] {
							c.ioOutput <- world[i][j]
						}
					}

					// Make sure that the Io has finished any output before exiting.
					c.ioCommand <- ioCheckIdle
					<-c.ioIdle
					c.events <- ImageOutputComplete{turn, outFile}
				case 'q':
					err := client.Call(stubs.PauseHandler, stubs.PauseRequest{}, new(stubs.PauseResponse))
					if err != nil {
						fmt.Println(err)
					}
				case 'k':
					err := client.Call(stubs.PauseHandler, stubs.PauseRequest{}, new(stubs.PauseResponse))
					if err != nil {
						fmt.Println(err)
					}
					err = client.Call(stubs.CloseHandler, stubs.CloseRequest{true}, new(stubs.CloseResponse))
					if err != nil {
						fmt.Println(err)
					}
				case 'p':
					err := client.Call(stubs.PauseHandler, stubs.PauseRequest{}, new(stubs.PauseResponse))
					if err != nil {
						fmt.Println(err)
					}
					paused = true
				}
			case <-keyListenerTriggers:
				break out
			}
		}
	}
}

// distributor divides the work between workers and interacts with other goroutines.
func distributor(params Params, channels distributorChannels) {
	if !inited {
		inited = true
		// AWS 98.80.10.53
		pBroker = flag.Arg(0)
		if !strings.Contains(pBroker, ":") {
			pBroker = "127.0.0.1:8030"
		}

		go ticker(2)
		go keyListener()

		var err error
		client, err = rpc.Dial("tcp", pBroker)
		if err != nil {
			panic(err)
		}

		go func() {
			response := new(stubs.AddressesResponse)
			client.Call(stubs.AddressesHandler, stubs.AddressesRequest{}, response)
			for _, addr := range response.Addresses {
				addresses <- addr
			}
		}()
	}

	p = params
	c = channels

	c.ioCommand <- ioInput
	c.ioFilename <- fmt.Sprintf("%dx%d", p.ImageWidth, p.ImageHeight)

	world := make([][]byte, p.ImageHeight)
	for i := range world {
		world[i] = make([]byte, p.ImageWidth)
	}

	for i := range world {
		for j := range world[i] {
			world[i][j] = <-c.ioInput
		}
	}

	turnsToRun = p.Turns

exe:
	c.events <- StateChange{p.Turns - turnsToRun, Executing}

	request := stubs.BreakWorldRequest{
		Turns:       turnsToRun,
		Threads:     p.Threads,
		ImageWidth:  p.ImageWidth,
		ImageHeight: p.ImageHeight,
		World:       world,
	}
	response := new(stubs.BreakWorldResponse)

	keyListenerTriggers <- true
	tickerTriggers <- true
	err := client.Call(stubs.BreakWorldHandler, request, response)
	tickerTriggers <- true
	keyListenerTriggers <- true

	for err != nil {
		fmt.Println(err)
		select {
		case addr := <-addresses:
			client, err = rpc.Dial("tcp", addr)
			if err != nil {
				continue
			}
			backupM.Lock()
			if backupTurns > 0 {
				turnsToRun = p.Turns - backupTurns
				request.Turns = turnsToRun
				request.World = backupWorld
			}
			backupM.Unlock()
			response = new(stubs.BreakWorldResponse)

			keyListenerTriggers <- true
			tickerTriggers <- true
			err = client.Call(stubs.BreakWorldHandler, request, response)
			tickerTriggers <- true
			keyListenerTriggers <- true
		case <-time.After(2 * time.Second):
			panic(err)
		}
	}

	world = response.World
	turnsToRun -= response.CompletedTurns

	currTurns := p.Turns - turnsToRun

	outputFile := func() {
		outFile := fmt.Sprintf("%dx%dx%d", p.ImageWidth, p.ImageHeight, currTurns)
		c.ioCommand <- ioOutput
		c.ioFilename <- outFile

		for i := range response.World {
			for j := range response.World[i] {
				c.ioOutput <- response.World[i][j]
			}
		}

		// Make sure that the Io has finished any output before exiting.
		c.ioCommand <- ioCheckIdle
		<-c.ioIdle
		c.events <- ImageOutputComplete{currTurns, outFile}
	}

	if paused {
		c.events <- StateChange{currTurns, Paused}
		keyListenerTriggers <- true
	}

	for paused {
		switch <-pauseKeyPresses {
		case 's':
			outputFile()
			keyListenerTriggers <- false
		case 'q':
			paused = false
			keyListenerTriggers <- true
		case 'k':
			err = client.Call(stubs.CloseHandler, stubs.CloseRequest{}, new(stubs.CloseResponse))
			if err != nil {
				panic(err)
			}
			paused = false
			keyListenerTriggers <- true
		case 'p':
			paused = false
			keyListenerTriggers <- true
			goto exe
		}
	}

	c.events <- FinalTurnComplete{currTurns, response.AliveCells}

	outputFile()

	c.events <- StateChange{currTurns, Quitting}

	// Close the channel to stop the SDL goroutine gracefully. Removing may cause deadlock.
	close(c.events)
}
