package gol

import (
	"fmt"
	"math"
	"sync"
	"time"

	"uk.ac.bris.cs/gameoflife/util"
)

type distributorChannels struct {
	events     chan<- Event
	ioCommand  chan<- ioCommand
	ioIdle     <-chan bool
	ioFilename chan<- string
	ioOutput   chan<- uint8
	ioInput    <-chan uint8
	keyPresses <-chan rune
}

// Step 1 functions --------------------
var directions = [8][2]int{
	// we need 8 neighbors, so 8 directions for every element in the 2D array
	{-1, 0},  //up
	{1, 0},   //down
	{0, -1},  //left
	{0, 1},   //right
	{-1, -1}, //left-up
	{-1, 1},  //right-up
	{1, -1},  //left-down
	{1, 1},   //right-down
}

func mod(num, mod int) int {
	return num % mod
}

func cond(num, bound int) int {
	if num < 0 {
		num += bound
	} else if num >= bound {
		num -= bound
	}

	return num
}

func calculateNextState(startY, endY, startX, endX int, world [][]byte, p Params, events chan<- Event, turn int) [][]byte {

	// variables for height and width of the slice we are working with
	height := endY - startY
	width := endX - startX

	// 2d array for the updated state as the world
	nextWorld := make([][]byte, height)
	// allocate inner arrays of nextWorld
	for i := 0; i < height; i++ {
		nextWorld[i] = make([]byte, width)
	}

	// we have to visit every element of the 2D array and compute the number of neighbours alive
	// we go by height first, after that we iterate over columns
	for i := 0; i < height; i++ {
		for j := 0; j < width; j++ {

			// variable for the number of alive neighbours
			aliveNeighbours := 0

			// check each neighbour by going over every possible direction of a neighbour
			for _, direction := range directions {
				// by replacing modulo by conditional statements, test speed improved 7.897s -> 7.188s
				dy := i + startY + direction[1]
				dx := j + startX + direction[0]

				// Unused modulo operation - see benchmarking
				//ni := mod(dy+p.ImageHeight, p.ImageHeight)
				//nj := mod(dx+p.ImageWidth, p.ImageWidth)

				ni := cond(dy, p.ImageHeight)
				nj := cond(dx, p.ImageWidth)

				// check whether the neighbour indexes aren't out of bounds
				if ni >= 0 && ni < p.ImageHeight && nj >= 0 && nj < p.ImageWidth {
					// check whether the neighbour is alive, if yes, increment the counter
					if world[ni][nj] == 255 {
						aliveNeighbours++
					}
				}
			}

			currentCellState := world[i+startY][j+startX]
			nextState := currentCellState
			// change current cell according to the neighbouring cells
			if aliveNeighbours < 2 || aliveNeighbours > 3 {
				nextState = 0
			} else if aliveNeighbours == 3 {
				nextState = 255
			}

			if currentCellState != nextState {
				events <- CellFlipped{turn, util.Cell{Y: i + startY, X: j + startX}}
			}
			nextWorld[i][j] = nextState
		}
	}
	return nextWorld
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

// --------------------------------------------

// Step 2 functions --------------------
func golWorker(startY, endY, startX, endX int, world [][]byte, p Params, events chan<- Event, out chan<- [][]byte, turn int) {
	nextWorld := calculateNextState(startY, endY, startX, endX, world, p, events, turn)
	out <- nextWorld
}

// --------------------------------------------

// Step 3 functions --------------------
func tickerEvent(c distributorChannels, turns *int, worldState *[][]byte, mu *sync.Mutex, done <-chan bool) {
	ticker := time.NewTicker(2 * time.Second)
	for {
		select {
		case <-ticker.C:
			mu.Lock() // is it needed
			aliveCells := calculateAliveCellsNumber(*worldState)
			currentTurn := *turns
			mu.Unlock()

			c.events <- AliveCellsCount{currentTurn, aliveCells}

			// make new helper to just calculate the int of alive cells
		case <-done: // stop ticker and return once the whole game is done
			ticker.Stop()
			return
		}
	}
}

func calculateAliveCellsNumber(world [][]byte) int {
	aliveCellsNumber := 0
	for i, worldCell := range world {
		for j := range worldCell {
			// if the current cell is alive, increase counter by one
			if world[i][j] == 255 {
				aliveCellsNumber++
			}
		}
	}
	return aliveCellsNumber
}

// --------------------------------------------

// Step 4 functions --------------------
// writeWorld outputs
func writeWorld(worldSlice [][]byte, c distributorChannels, p Params, completedTurns int) {
	c.ioCommand <- ioOutput
	filename := fmt.Sprintf("%dx%dx%d", p.ImageWidth, p.ImageHeight, completedTurns)
	c.ioFilename <- filename
	for i := range worldSlice {
		for j := range worldSlice[i] {
			c.ioOutput <- worldSlice[i][j]
		}
	}
	c.ioCommand <- ioCheckIdle
	<-c.ioIdle
	c.events <- ImageOutputComplete{completedTurns, filename}
}

// --------------------------------------------

// distributor divides the work between workers and interacts with other goroutines.
func distributor(p Params, c distributorChannels) {

	c.ioCommand <- ioInput
	c.ioFilename <- fmt.Sprintf("%dx%d", p.ImageWidth, p.ImageHeight)

	// Initialise variables --------------------

	// 2D slice to store the state of the world
	worldSlice := make([][]byte, p.ImageHeight)
	for i := range worldSlice {
		worldSlice[i] = make([]byte, p.ImageWidth)
	}
	// Populate the world slice with the image from IO
	for i := range worldSlice {
		for j := range worldSlice[i] {
			worldSlice[i][j] = <-c.ioInput
		}
	}

	// Channels for the worker threads to communicate upon
	channels := make([]chan [][]byte, p.Threads)
	for i := range channels {
		channels[i] = make(chan [][]byte)
	}

	div := float64(p.ImageHeight) / float64(p.Threads)
	turn := 0
	done := make(chan bool) // Channel to notify the ticker when the GoL has finished
	mu := &sync.Mutex{}
	paused := false // Keep track of whether the state is paused

	// Execute all turns of the Game of Life. --------------------

	// Send initially alive cells, state change, and start timer
	c.events <- CellsFlipped{turn, calculateAliveCells(worldSlice)}
	c.events <- StateChange{turn, Executing}
	go tickerEvent(c, &turn, &worldSlice, mu, done)

	runNext := func() {
		if !paused {
			// Send each section of the state to a new worker thread
			for i := 0; i < p.Threads; i++ {
				go golWorker(int(math.Round(div*float64(i))), int(math.Round(div*float64(i+1))), 0, p.ImageWidth, worldSlice, p, c.events, channels[i], turn)
			}

			// Create a new slice to hold the worker output
			var newWorldSlice [][]byte

			for i := 0; i < p.Threads; i++ {
				output := <-channels[i]
				newWorldSlice = append(newWorldSlice, output...)
			}

			// Mutex lock required as the ticker also reads from these
			mu.Lock()
			worldSlice = newWorldSlice
			turn++
			mu.Unlock()
			c.events <- TurnComplete{turn}
		}
	}

out:
	for turn < p.Turns {
		select {
		case input := <-c.keyPresses:
			switch input {
			case 's':
				writeWorld(worldSlice, c, p, turn)
			case 'q':
				break out
			case 'p':
				if paused == false {
					paused = true
					c.events <- StateChange{turn, Paused}
				} else {
					paused = false
					c.events <- StateChange{turn, Executing}
				}
			default:
				runNext()
			}
		default:
			runNext()
		}
	}

	// Exit the simulation --------------------

	// Stop the ticker and report the final state, and write it to the output channel
	done <- true
	c.events <- FinalTurnComplete{turn, calculateAliveCells(worldSlice)}
	writeWorld(worldSlice, c, p, turn)

	// Make sure that the Io has finished any output before exiting.
	c.ioCommand <- ioCheckIdle
	<-c.ioIdle

	c.events <- StateChange{turn, Quitting}

	// Close the channel to stop the SDL goroutine gracefully. Removing may cause deadlock.
	close(c.events)
}
