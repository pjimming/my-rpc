package main

import (
	"encoding/json"
	"fmt"
	myrpc "github.com/pjimming/my-rpc"
	"github.com/pjimming/my-rpc/codec"
	"log"
	"net"
	"time"
)

func main() {
	addr := make(chan string)
	go startServer(addr)

	// in fact, following code is like a simple my-rpc client
	conn, _ := net.Dial("tcp", <-addr)
	defer func() { _ = conn.Close() }()

	time.Sleep(time.Second)
	// send option
	_ = json.NewEncoder(conn).Encode(myrpc.DefaultOption)
	cc := codec.NewGobCodec(conn)
	for i := 0; i < 5; i++ {
		h := &codec.Header{
			ServiceMethod: "Foo.Sum",
			Seq:           uint64(i),
		}
		_ = cc.Write(h, fmt.Sprintf("my-rpc req %d", h.Seq))
		_ = cc.ReadHeader(h)
		var reply string
		_ = cc.ReadBody(&reply)
		log.Printf("reply: %s", reply)
	}
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
