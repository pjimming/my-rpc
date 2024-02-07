package main

import (
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	myrpc "github.com/pjimming/my-rpc"
)

func main() {
	addr := make(chan string)
	go startServer(addr)

	// in fact, following code is like a simple my-rpc client
	client, _ := myrpc.Dial("tcp", <-addr)
	defer func() { _ = client.Close() }()

	time.Sleep(time.Second)
	// send option
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			args := fmt.Sprintf("my-rpc req %d", i)
			var reply string
			if err := client.Call("Foo.Sum", args, &reply); err != nil {
				log.Fatalf("call Foo.Sum fail, %v", err)
			}
			log.Printf("reply: %s", reply)
		}(i)
	}
	wg.Wait()
}

func startServer(addr chan string) {
	// pick a free port
	l, err := net.Listen("tcp", ":0")
	if err != nil {
		log.Fatalf("network error: %v", err)
	}

	log.Printf("start rpc server on: %s", l.Addr())
	addr <- l.Addr().String()
	myrpc.Accept(l)
}
