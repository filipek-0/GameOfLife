package stubs

import (
	"uk.ac.bris.cs/gameoflife/util"
)

var (
	SubscribeHandler    = "GOLService.Subscribe"
	AddressesHandler    = "GOLService.Addresses"
	BreakWorldHandler   = "GOLService.BreakWorld"
	CurrentStateHandler = "GOLService.CurrentState"
	PauseHandler        = "GOLService.Pause"
	CloseHandler        = "GOLService.Close"

	RunWorldHandler = "GOLService.RunWorld"
)

type SubscribeResponse struct{}

type SubscribeRequest struct {
	Address string
}

type AddressesResponse struct {
	Addresses []string
}

type AddressesRequest struct{}

type BreakWorldResponse struct {
	CompletedTurns int
	World          [][]byte
	AliveCells     []util.Cell
}

type BreakWorldRequest struct {
	Turns       int
	Threads     int
	ImageWidth  int
	ImageHeight int
	World       [][]byte
}

type RunWorldResponse struct {
	WorldSlice [][]byte
}

type RunWorldRequest struct {
	Width      int
	Height     int
	WorldSlice [][]byte
}

type CurrentStateResponse struct {
	CompletedTurns int
	World          [][]byte
	CellsCount     int
}

type CurrentStateRequest struct{}

type PauseResponse struct{}

type PauseRequest struct{}

type CloseResponse struct{}

type CloseRequest struct {
	IsBroker bool
}
