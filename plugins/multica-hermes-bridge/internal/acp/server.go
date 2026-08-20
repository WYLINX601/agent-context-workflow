package acp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/linx-workbench/multica-hermes-gateway/internal/protocol"
)

const (
	JSONRPCInvalidRequest = -32600
	JSONRPCMethodNotFound = -32601
	JSONRPCInvalidParams  = -32602
	JSONRPCInternalError  = -32603
)

type RPCError struct {
	Code    int
	Message string
	Data    any
}

func (e *RPCError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

func (e *RPCError) wire() *protocol.Error {
	if e == nil {
		return nil
	}
	data, _ := json.Marshal(e.Data)
	return &protocol.Error{Code: e.Code, Message: e.Message, Data: data}
}

type Emitter interface {
	Notify(ctx context.Context, method string, params any) error
	Request(ctx context.Context, method string, params any) (json.RawMessage, error)
}

type Handler interface {
	Handle(ctx context.Context, method string, params json.RawMessage, emitter Emitter) (any, *RPCError)
	Shutdown(ctx context.Context)
}

type Server struct {
	in      io.Reader
	out     io.Writer
	logger  func(format string, args ...any)
	handler Handler

	writeMu   sync.Mutex
	pendingMu sync.Mutex
	pending   map[string]chan protocol.Message
	nextID    atomic.Uint64
	stop      chan struct{}
	done      chan struct{}
	cancelMu  sync.Mutex
	cancel    context.CancelFunc
	handlers  sync.WaitGroup
	closeOnce sync.Once
}

func NewServer(in io.Reader, out io.Writer, handler Handler, logger func(string, ...any)) *Server {
	if logger == nil {
		logger = func(string, ...any) {}
	}
	return &Server{
		in:      in,
		out:     out,
		logger:  logger,
		handler: handler,
		pending: make(map[string]chan protocol.Message),
		stop:    make(chan struct{}),
		done:    make(chan struct{}),
	}
}

func (s *Server) Run(ctx context.Context) error {
	defer close(s.done)
	runCtx, cancel := context.WithCancel(ctx)
	s.cancelMu.Lock()
	s.cancel = cancel
	s.cancelMu.Unlock()
	defer cancel()
	go func() {
		select {
		case <-runCtx.Done():
			s.Close()
		case <-s.stop:
		}
	}()

	lines := make(chan []byte)
	scanDone := make(chan error, 1)
	go func() {
		scanner := bufio.NewScanner(s.in)
		scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
		for scanner.Scan() {
			line := append([]byte(nil), scanner.Bytes()...)
			select {
			case lines <- line:
			case <-runCtx.Done():
				scanDone <- runCtx.Err()
				return
			}
		}
		scanDone <- scanner.Err()
	}()
readLoop:
	for {
		select {
		case line := <-lines:
			if len(line) == 0 {
				continue
			}
			var message protocol.Message
			if err := json.Unmarshal(line, &message); err != nil {
				s.logger("invalid ACP JSON: %v", err)
				_ = s.write(protocol.NewError(nil, &protocol.Error{Code: JSONRPCInvalidRequest, Message: "invalid JSON-RPC message"}))
				continue
			}
			if len(message.ID) > 0 && message.Method == "" {
				s.resolve(message)
				continue
			}
			if message.Method == "" {
				if len(message.ID) > 0 {
					_ = s.write(protocol.NewError(message.ID, &protocol.Error{Code: JSONRPCInvalidRequest, Message: "request method is required"}))
				}
				continue
			}
			s.handlers.Add(1)
			go func() {
				defer s.handlers.Done()
				s.dispatch(runCtx, message)
			}()
		case err := <-scanDone:
			if err != nil && !errors.Is(err, io.EOF) {
				s.logger("ACP stdin read failed: %v", err)
			}
			break readLoop
		case <-runCtx.Done():
			break readLoop
		}
	}
	s.Close()
	s.handlers.Wait()
	return nil
}

func (s *Server) dispatch(ctx context.Context, message protocol.Message) {
	result, rpcErr := s.handler.Handle(ctx, message.Method, message.Params, s)
	if len(message.ID) == 0 {
		return
	}
	if rpcErr != nil {
		_ = s.write(protocol.NewError(message.ID, rpcErr.wire()))
		return
	}
	response, err := protocol.NewResult(message.ID, result)
	if err != nil {
		_ = s.write(protocol.NewError(message.ID, &protocol.Error{Code: JSONRPCInternalError, Message: "encode response failed"}))
		return
	}
	_ = s.write(response)
}

func (s *Server) Notify(ctx context.Context, method string, params any) error {
	message, err := protocol.NewNotification(method, params)
	if err != nil {
		return err
	}
	return s.writeContext(ctx, message)
}

func (s *Server) Request(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := s.nextID.Add(1)
	message, err := protocol.NewRequest(id, method, params)
	if err != nil {
		return nil, err
	}
	key := strconv.FormatUint(id, 10)
	response := make(chan protocol.Message, 1)
	s.pendingMu.Lock()
	s.pending[key] = response
	s.pendingMu.Unlock()
	defer func() {
		s.pendingMu.Lock()
		delete(s.pending, key)
		s.pendingMu.Unlock()
	}()
	if err := s.writeContext(ctx, message); err != nil {
		return nil, err
	}
	select {
	case reply := <-response:
		if reply.Error != nil {
			return nil, reply.Error
		}
		return append(json.RawMessage(nil), reply.Result...), nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.stop:
		return nil, errors.New("ACP server is closed")
	}
}

func (s *Server) resolve(message protocol.Message) {
	key := protocol.IDKey(message.ID)
	s.pendingMu.Lock()
	channel := s.pending[key]
	s.pendingMu.Unlock()
	if channel == nil {
		s.logger("received response for unknown ACP request id=%s", key)
		return
	}
	select {
	case channel <- message:
	default:
	}
}

func (s *Server) write(message protocol.Message) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	data, err := json.Marshal(message)
	if err != nil {
		return err
	}
	if _, err := s.out.Write(append(data, '\n')); err != nil {
		return err
	}
	if flusher, ok := s.out.(interface{ Flush() error }); ok {
		return flusher.Flush()
	}
	return nil
}

func (s *Server) writeContext(ctx context.Context, message protocol.Message) error {
	if deadline, ok := ctx.Deadline(); ok {
		if time.Until(deadline) <= 0 {
			return ctx.Err()
		}
	}
	select {
	case <-s.stop:
		return errors.New("ACP server is closed")
	default:
		return s.write(message)
	}
}

func (s *Server) Close() {
	s.closeOnce.Do(func() {
		close(s.stop)
		s.cancelMu.Lock()
		if s.cancel != nil {
			s.cancel()
		}
		s.cancelMu.Unlock()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if s.handler != nil {
			s.handler.Shutdown(ctx)
		}
	})
}

func (s *Server) Wait() {
	<-s.done
}

func InvalidParams(message string, data any) *RPCError {
	return &RPCError{Code: JSONRPCInvalidParams, Message: message, Data: data}
}

func MethodNotFound(method string) *RPCError {
	return &RPCError{Code: JSONRPCMethodNotFound, Message: fmt.Sprintf("method not found: %s", method)}
}

func Internal(message string, data any) *RPCError {
	return &RPCError{Code: JSONRPCInternalError, Message: message, Data: data}
}

func NewRPCError(code int, message string, data any) *RPCError {
	return &RPCError{Code: code, Message: message, Data: data}
}

var _ Emitter = (*Server)(nil)
