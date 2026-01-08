package main

import (
	"flag"
	"fmt"
	"net"
	"net/rpc"
	"time"

	"uk.ac.bris.cs/gameoflife/stubs"
)

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
	pAddr := flag.String("port", "8030", "Port to listen on")
	pBroker := flag.String("broker", "127.0.0.1:8030", "IP:port string to connect to as broker")
	ec2Addr := flag.String("ip", "127.0.0.1", "IP of the ec2 instance")
	flag.Parse()

	addr := *ec2Addr + ":" + *pAddr

	var client *rpc.Client
	var err error
	addrs := make([]string, 0)
	if addr != *pBroker {
		client, err = rpc.Dial("tcp", *pBroker)
		if err != nil {
			panic(err)
		}

		req := stubs.AddressesRequest{}
		res := new(stubs.AddressesResponse)
		err = client.Call(stubs.AddressesHandler, req, res)
		if err != nil {
			panic(err)
		}
		addrs = res.Addresses
	}

	service := GOLService{
		Addrs:      addrs,
		Clients:    make([]*rpc.Client, 0),
		Interrupts: make(chan bool),
		Finished:   make(chan bool),
		Closes:     make(chan bool),
	}
	rpc.Register(&service)
	listener, _ := net.Listen("tcp", ":"+*pAddr)
	go rpc.Accept(listener)

	if client != nil {
		reqS := stubs.SubscribeRequest{Address: addr}
		resS := new(stubs.SubscribeResponse)
		err = client.Call(stubs.SubscribeHandler, reqS, resS)
		if err != nil {
			panic(err)
		}
		client.Close()
	}

	for _, other := range addrs {
		client, err = rpc.Dial("tcp", other)
		if err != nil {
			fmt.Println(err)
			continue
		}
		req := stubs.SubscribeRequest{Address: addr}
		res := new(stubs.SubscribeResponse)
		err = client.Call(stubs.SubscribeHandler, req, res)
		if err != nil {
			fmt.Println(err)
		}
		client.Close()
	}

	if <-service.Closes {
		for _, client = range service.Clients {
			client.Call(stubs.CloseHandler, stubs.CloseRequest{}, new(stubs.CloseResponse))
			client.Close()
		}
	}
	<-time.After(500 * time.Millisecond)
	listener.Close()
}
