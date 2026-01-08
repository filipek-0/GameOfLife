package gol

import (
	"flag"
	"fmt"
	"net/rpc"
	"time"

	"uk.ac.bris.cs/gameoflife/stubs"
)

var (
	inited = false
	paused = false

	pBroker *string
	client  *rpc.Client

	p Params
	c distributorChannels

	tickerTriggers      = make(chan bool)
	keyListenerTriggers = make(chan bool)
	pauseKeyPresses     = make(chan rune)

	turns int
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

// ticker report the number of cells that are still alive every 2 seconds when gol is running
func ticker(seconds time.Duration) {
	for {
		<-tickerTriggers
	out:
		for {
			select {
			case <-time.After(seconds * time.Second):
				request := stubs.CountAliveRequest{}
				response := new(stubs.CountAliveResponse)
				err := client.Call(stubs.CountAliveHandler, request, response)
				if err != nil {
					panic(err)
				}

				c.events <- AliveCellsCount{
					p.Turns - turns + response.CompletedTurns,
					response.CellsCount,
				}
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
					response := new(stubs.CurrentStateResponse)
					err := client.Call(stubs.CurrentStateHandler, stubs.CurrentStateRequest{}, response)
					if err != nil {
						panic(err)
					}

					outFile := fmt.Sprintf("%dx%dx%d", p.ImageWidth, p.ImageHeight, response.CompletedTurns)
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
					c.events <- ImageOutputComplete{response.CompletedTurns, outFile}
				case 'q':
					err := client.Call(stubs.PauseHandler, stubs.PauseRequest{}, new(stubs.PauseResponse))
					if err != nil {
						panic(err)
					}
				case 'k':
					err := client.Call(stubs.PauseHandler, stubs.PauseRequest{}, new(stubs.PauseResponse))
					if err != nil {
						panic(err)
					}
					err = client.Call(stubs.BrokerCloseHandler, stubs.CloseRequest{}, new(stubs.CloseResponse))
					if err != nil {
						panic(err)
					}
				case 'p':
					err := client.Call(stubs.PauseHandler, stubs.PauseRequest{}, new(stubs.PauseResponse))
					if err != nil {
						panic(err)
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
		pBroker = flag.String("broker", "127.0.0.1:8030", "IP:port string to connect to as broker")
		flag.Parse()

		go ticker(2)
		go keyListener()
	}

	p = params
	c = channels

	client, _ = rpc.Dial("tcp", *pBroker)
	defer client.Close()

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

	turns = p.Turns

exe:
	err := client.Call(stubs.PreBreakHandler, stubs.PreBreakRequest{}, new(stubs.PreBreakResponse))
	if err != nil {
		panic(err)
	}
	c.events <- StateChange{p.Turns - turns, Executing}

	request := stubs.BreakWorldRequest{
		Turns:       turns,
		Threads:     p.Threads,
		ImageWidth:  p.ImageWidth,
		ImageHeight: p.ImageHeight,
		World:       world,
	}
	response := new(stubs.BreakWorldResponse)

	keyListenerTriggers <- true
	tickerTriggers <- true
	err = client.Call(stubs.BreakWorldHandler, request, response)
	if err != nil {
		panic(err)
	}
	tickerTriggers <- true
	keyListenerTriggers <- true

	currTurns := p.Turns - turns + response.CompletedTurns

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
			err = client.Call(stubs.BrokerCloseHandler, stubs.CloseRequest{}, new(stubs.CloseResponse))
			if err != nil {
				panic(err)
			}
			paused = false
			keyListenerTriggers <- true
		case 'p':
			paused = false
			world = response.World
			turns -= response.CompletedTurns
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
