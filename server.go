package my_rpc

import (
	"encoding/gob"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/pjimming/my-rpc/codec"
)

type Option struct {
	MagicNumber    int           // MagicNumber marks this is a my-rpc request
	CodecType      codec.Type    // client may choose different Codec to encode body
	ConnectTimeout time.Duration // 0 means no limit
	HandleTimeout  time.Duration
}

var DefaultOption = &Option{
	MagicNumber:    MagicNumber,
	CodecType:      codec.GodType,
	ConnectTimeout: 10 * time.Second,
}

// Server represents an RPC Server.
type Server struct {
	serviceMap sync.Map
}

// NewServer returns a new Server.
func NewServer() *Server {
	return &Server{}
}

// DefaultServer is the default instance of *Server.
var DefaultServer = NewServer()

func (server *Server) Register(rcvr interface{}) error {
	s := newService(rcvr)
	if _, dup := server.serviceMap.LoadOrStore(s.name, s); dup {
		return fmt.Errorf("rpc: service %s already defined", s.name)
	}
	return nil
}

func Register(rcvr interface{}) error { return DefaultServer.Register(rcvr) }

// Accept accepts connection on the listener and serves requests
// for each incoming connection
func (server *Server) Accept(lis net.Listener) {
	for {
		conn, err := lis.Accept()
		if err != nil {
			log.Printf("rpc server: accept error: %v", err)
			return
		}
		go server.ServeConn(conn)
	}
}

// Accept accepts connections on the listener and serves requests
// for each incoming connection.
func Accept(lis net.Listener) { DefaultServer.Accept(lis) }

func (server *Server) ServeConn(conn io.ReadWriteCloser) {
	defer func() { _ = conn.Close() }()
	var opt Option
	if err := gob.NewDecoder(conn).Decode(&opt); err != nil {
		log.Printf("rpc server: options error: %v", err)
		return
	}

	if opt.MagicNumber != MagicNumber {
		log.Printf("rpc server: invalid magic number %x", opt.MagicNumber)
		return
	}

	f := codec.NewCodecFuncMap[opt.CodecType]
	if f == nil {
		log.Printf("rpc server: invalid codec type %server", opt.CodecType)
		return
	}
	server.serveCodec(f(conn), opt.HandleTimeout)
}

var invalidRequest = struct {
}{}

// 1. readRequest
// 2. handleRequest
// 3. sendResponse
func (server *Server) serveCodec(cc codec.Codec, timeout time.Duration) {
	sending := new(sync.Mutex)
	wg := new(sync.WaitGroup)
	for {
		req, err := server.readRequest(cc)
		if err != nil {
			if req == nil {
				// it'server not possible to recover, so close the connection
				break
			}
			req.h.Error = err.Error()
			server.sendResponse(cc, req.h, invalidRequest, sending)
			continue
		}
		wg.Add(1)
		go server.handleRequest(cc, req, sending, wg, timeout)
	}
	wg.Wait()
	_ = cc.Close()
}

type request struct {
	h            *codec.Header // header of request
	argv, replyv reflect.Value
	mType        *methodType
	svc          *service
}

func (server *Server) readRequestHeader(cc codec.Codec) (*codec.Header, error) {
	var h codec.Header
	if err := cc.ReadHeader(&h); err != nil {
		if !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
			log.Printf("rpc server: read header error: %v", err)
			return nil, err
		}
	}
	return &h, nil
}

func (server *Server) readRequest(cc codec.Codec) (*request, error) {
	h, err := server.readRequestHeader(cc)
	if err != nil {
		return nil, err
	}

	req := &request{h: h}
	req.svc, req.mType, err = server.findService(h.ServiceMethod)
	if err != nil {
		return req, err
	}

	req.argv = req.mType.newArgv()
	req.replyv = req.mType.newReplyv()

	// make sure that argvi is a pointer, ReadBody need a pointer as parameter
	argvi := req.argv.Interface()
	if req.argv.Type().Kind() != reflect.Ptr {
		argvi = req.argv.Addr().Interface()
	}
	if err = cc.ReadBody(argvi); err != nil {
		log.Printf("rpc server: read argv error: %v", err)
		return req, err
	}
	return req, nil
}

func (server *Server) sendResponse(cc codec.Codec, h *codec.Header, body interface{}, sending *sync.Mutex) {
	sending.Lock()
	defer sending.Unlock()
	if err := cc.Write(h, body); err != nil {
		log.Printf("rpc server: wirte response error: %v", err)
	}
}

func (server *Server) handleRequest(cc codec.Codec, req *request, sending *sync.Mutex, wg *sync.WaitGroup, timeout time.Duration) {
	defer wg.Done()
	called := make(chan struct{})
	sent := make(chan struct{})
	finish := make(chan struct{})
	defer close(finish)

	go func() {
		err := req.svc.call(req.mType, req.argv, req.replyv)
		select {
		case <-finish:
			// avoid goroutine memory leak
			close(called)
			close(sent)
			return
		case called <- struct{}{}:
			if err != nil {
				req.h.Error = err.Error()
				server.sendResponse(cc, req.h, invalidRequest, sending)
				sent <- struct{}{}
				return
			}
			server.sendResponse(cc, req.h, req.replyv.Interface(), sending)
			sent <- struct{}{}
		}
	}()

	if timeout == 0 {
		<-called
		<-sent
		return
	}

	select {
	case <-time.After(timeout):
		req.h.Error = fmt.Sprintf("rpc server: request handle timeout: expect within %s", timeout.String())
		server.sendResponse(cc, req.h, invalidRequest, sending)
	case <-called:
		<-sent
	}
}

func (server *Server) findService(serviceMethod string) (svc *service, mType *methodType, err error) {
	dot := strings.LastIndex(serviceMethod, ".")
	if dot < 0 {
		err = fmt.Errorf("rpc server: service.method request ill-formed: %s", serviceMethod)
		return
	}

	serviceName, methodName := serviceMethod[:dot], serviceMethod[dot+1:]
	svci, ok := server.serviceMap.Load(serviceName)
	if !ok {
		err = fmt.Errorf("rpc server: can't find service %s", serviceName)
		return
	}

	svc = svci.(*service)
	mType = svc.method[methodName]
	if mType == nil {
		err = fmt.Errorf("rpc server: can't find method %s", methodName)
	}
	return
}

// ServeHTTP implements an http.Handler that answers RPC requests.
func (server *Server) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodConnect {
		w.Header().Set("content-type", "text/plain: charset=utf-8")
		w.WriteHeader(http.StatusMethodNotAllowed)
		_, _ = io.WriteString(w, methodNotAllowed+"\n")
		return
	}

	conn, _, err := w.(http.Hijacker).Hijack()
	if err != nil {
		log.Printf("rpc hijacking %s fail, %v", req.RemoteAddr, err)
		return
	}

	_, _ = io.WriteString(conn, "HTTP/1.1 "+connected+"\n\n")
	server.ServeConn(conn)
}

// HandleHTTP registers an HTTP handler for RPC messages on rpcPath.
// It is still necessary to invoke http.Serve(), typically in a go statement.
func (server *Server) HandleHTTP() {
	http.Handle(defaultRPCPath, server)
	http.Handle(defaultDebugPath, debugHTTP{server})
	log.Printf("rpc server debug path: %s", defaultDebugPath)
}

// HandleHTTP is a convenient approach for default server to register HTTP handlers
func HandleHTTP() {
	DefaultServer.HandleHTTP()
}
