package stubs

import (
	"uk.ac.bris.cs/gameoflife/util"
)

var (
	SubscribeHandler    = "Broker.Subscribe"
	PreBreakHandler     = "Broker.PreBreak"
	BreakWorldHandler   = "Broker.BreakWorld"
	CountAliveHandler   = "Broker.CountAlive"
	CurrentStateHandler = "Broker.CurrentState"
	PauseHandler        = "Broker.Pause"
	BrokerCloseHandler  = "Broker.Close"

	RunWorldHandler    = "GOLOperations.RunWorld"
	WorkerCloseHandler = "GOLOperations.Close"
)

type SubscribeResponse struct{}

type SubscribeRequest struct {
	Address string
}

type PreBreakResponse struct{}

type PreBreakRequest struct{}

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

type CountAliveResponse struct {
	CompletedTurns int
	CellsCount     int
}

type CountAliveRequest struct{}

type CurrentStateResponse struct {
	CompletedTurns int
	World          [][]byte
}

type CurrentStateRequest struct{}

type PauseResponse struct{}

type PauseRequest struct{}

type CloseResponse struct{}

type CloseRequest struct{}
