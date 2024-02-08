package my_rpc

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"reflect"
	"strings"
	"sync"

	"github.com/pjimming/my-rpc/codec"
)

const MagicNumber = 998244353

type Option struct {
	MagicNumber int        // MagicNumber marks this is a my-rpc request
	CodecType   codec.Type // client may choose different Codec to encode body
}

var DefaultOption = &Option{
	MagicNumber: MagicNumber,
	CodecType:   codec.GodType,
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
	if err := json.NewDecoder(conn).Decode(&opt); err != nil {
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
	server.serveCodec(f(conn))
}

var invalidRequest = struct {
}{}

// 1. readRequest
// 2. handleRequest
// 3. sendRequest
func (server *Server) serveCodec(cc codec.Codec) {
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
		go server.handleResponse(cc, req, sending, wg)
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

func (server *Server) handleResponse(cc codec.Codec, req *request, sending *sync.Mutex, wg *sync.WaitGroup) {
	defer wg.Done()
	err := req.svc.call(req.mType, req.argv, req.replyv)
	if err != nil {
		req.h.Error = err.Error()
		server.sendResponse(cc, req.h, invalidRequest, sending)
		return
	}
	server.sendResponse(cc, req.h, req.replyv.Interface(), sending)
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
