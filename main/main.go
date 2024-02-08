package main

import (
	"context"
	myrpc "github.com/pjimming/my-rpc"
	"log"
	"net"
	"net/http"
	"sync"
	"time"
)

type Foo int

type Args struct {
	Num1, Num2 int
}

func (f Foo) Sum(args Args, reply *int) error {
	*reply = args.Num1 + args.Num2
	return nil
}

func main() {
	addr := make(chan string)
	go call(addr)
	startServer(addr)
}

func call(addrCh chan string) {
	// in fact, following code is like a simple my-rpc client
	client, _ := myrpc.DialHTTP("tcp", <-addrCh)
	defer func() { _ = client.Close() }()

	time.Sleep(time.Second)
	// send option
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			args := &Args{
				Num1: i,
				Num2: i * i,
			}
			var reply int
			if err := client.Call(context.Background(), "Foo.Sum", args, &reply); err != nil {
				log.Fatalf("call Foo.Sum fail, %v", err)
			}
			log.Printf("%d + %d = %d", args.Num1, args.Num2, reply)
		}(i)
	}
	wg.Wait()
}

func startServer(addr chan string) {
	var foo Foo
	if err := myrpc.Register(&foo); err != nil {
		log.Fatalf("register fail: %v", err)
	}
	// pick a free port
	l, err := net.Listen("tcp", ":9999")
	if err != nil {
		log.Fatalf("network error: %v", err)
	}
	myrpc.HandleHTTP()
	log.Printf("start rpc server on: %s", l.Addr())
	addr <- l.Addr().String()
	_ = http.Serve(l, nil)
}
