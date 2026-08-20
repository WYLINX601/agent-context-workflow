package supervisor

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"time"
)

type Server struct {
	manager   *Manager
	endpoint  string
	listener  net.Listener
	closeOnce sync.Once
	done      chan struct{}
}

func NewServer(manager *Manager, endpoint string) (*Server, error) {
	listener, err := listenIPC(endpoint)
	if err != nil {
		return nil, err
	}
	return &Server{manager: manager, endpoint: endpoint, listener: listener, done: make(chan struct{})}, nil
}

func (s *Server) Serve() error {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.done:
				return nil
			default:
			}
			return err
		}
		go s.serveConn(conn)
	}
}

func (s *Server) Close() {
	s.closeOnce.Do(func() { close(s.done); _ = s.listener.Close(); closeIPC(s.endpoint) })
}

func (s *Server) serveConn(conn net.Conn) {
	defer conn.Close()
	reader := bufio.NewScanner(conn)
	reader.Buffer(make([]byte, 64*1024), 4*1024*1024)
	encoder := json.NewEncoder(conn)
	for reader.Scan() {
		var request Request
		if err := json.Unmarshal(reader.Bytes(), &request); err != nil {
			_ = encoder.Encode(errorResponse("", &SupervisorError{Code: CodeUnavailable, Message: "invalid supervisor IPC JSON"}))
			continue
		}
		response := s.dispatch(request)
		if err := encoder.Encode(response); err != nil {
			return
		}
	}
}

func (s *Server) dispatch(request Request) Response {
	if request.ProtocolVersion != ProtocolVersion || request.ClientID == "" || request.RequestID == "" {
		return errorResponse(request.RequestID, &SupervisorError{Code: CodeUnavailable, Message: "invalid supervisor IPC envelope"})
	}
	ctx := context.Background()
	switch request.Method {
	case "AcquireRuntime":
		var params AcquireParams
		if err := json.Unmarshal(request.Params, &params); err != nil {
			return errorResponse(request.RequestID, err)
		}
		result, err := s.manager.Acquire(ctx, request.ClientID, request.RequestID, params)
		if err != nil {
			return errorResponse(request.RequestID, err)
		}
		return Response{ProtocolVersion: ProtocolVersion, RequestID: request.RequestID, OK: true, Result: result}
	case "HeartbeatLease":
		var params LeaseParams
		if err := json.Unmarshal(request.Params, &params); err != nil {
			return errorResponse(request.RequestID, err)
		}
		err := s.manager.Heartbeat(ctx, request.ClientID, params.LeaseID, request.Generation)
		return responseForError(request.RequestID, err)
	case "PinTurn":
		var params LeaseParams
		if err := json.Unmarshal(request.Params, &params); err != nil {
			return errorResponse(request.RequestID, err)
		}
		err := s.manager.Pin(ctx, request.ClientID, params.LeaseID, params.SessionID, request.Generation)
		return responseForError(request.RequestID, err)
	case "UnpinTurn":
		var params LeaseParams
		if err := json.Unmarshal(request.Params, &params); err != nil {
			return errorResponse(request.RequestID, err)
		}
		err := s.manager.Unpin(ctx, request.ClientID, params.LeaseID, params.SessionID, request.Generation)
		return responseForError(request.RequestID, err)
	case "ReleaseRuntime":
		var params LeaseParams
		if err := json.Unmarshal(request.Params, &params); err != nil {
			return errorResponse(request.RequestID, err)
		}
		err := s.manager.Release(ctx, request.ClientID, params.LeaseID, request.Generation)
		return responseForError(request.RequestID, err)
	case "GetRuntimeStatus":
		var params LeaseParams
		if err := json.Unmarshal(request.Params, &params); err != nil {
			return errorResponse(request.RequestID, err)
		}
		result, err := s.manager.Status(ctx, params.RuntimeID)
		if err != nil {
			return errorResponse(request.RequestID, err)
		}
		return Response{ProtocolVersion: ProtocolVersion, RequestID: request.RequestID, OK: true, Result: result}
	case "GetOperationStatus":
		var params OperationParams
		if err := json.Unmarshal(request.Params, &params); err != nil {
			return errorResponse(request.RequestID, err)
		}
		result, err := s.manager.OperationStatus(ctx, request.ClientID, params)
		if err != nil {
			return errorResponse(request.RequestID, err)
		}
		return Response{ProtocolVersion: ProtocolVersion, RequestID: request.RequestID, OK: true, Result: result}
	case "StopRuntime":
		var params StopParams
		if err := json.Unmarshal(request.Params, &params); err != nil {
			return errorResponse(request.RequestID, err)
		}
		err := s.manager.Stop(ctx, request.ClientID, params.RuntimeID, request.Generation)
		return responseForError(request.RequestID, err)
	default:
		return errorResponse(request.RequestID, fmt.Errorf("unknown supervisor method %q", request.Method))
	}
}

func responseForError(requestID string, err error) Response {
	if err != nil {
		return errorResponse(requestID, err)
	}
	return Response{ProtocolVersion: ProtocolVersion, RequestID: requestID, OK: true, Result: map[string]any{"ok": true}}
}

type Client struct {
	endpoint string
	clientID string
	mu       sync.Mutex
}

func NewClient(endpoint, clientID string) *Client {
	return &Client{endpoint: endpoint, clientID: clientID}
}

func (c *Client) call(ctx context.Context, request Request, result any) error {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
	}
	conn, err := dialIPC(ctx, c.endpoint)
	if err != nil {
		return &SupervisorError{Code: CodeUnavailable, Message: "Supervisor IPC is unavailable"}
	}
	defer conn.Close()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	if err := json.NewEncoder(conn).Encode(request); err != nil {
		return &SupervisorError{Code: CodeUnavailable, Message: "write Supervisor IPC request failed"}
	}
	var response Response
	if err := json.NewDecoder(conn).Decode(&response); err != nil {
		return &SupervisorError{Code: CodeUnavailable, Message: "read Supervisor IPC response failed"}
	}
	if !response.OK {
		if response.Error == nil {
			return &SupervisorError{Code: CodeUnavailable, Message: "Supervisor returned an unspecified error"}
		}
		return &SupervisorError{Code: response.Error.Code, Message: response.Error.Message}
	}
	if result == nil {
		return nil
	}
	data, err := json.Marshal(response.Result)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, result)
}

func (c *Client) requestWithID(ctx context.Context, requestID, method string, generation uint64, params any, result any) error {
	data, err := json.Marshal(params)
	if err != nil {
		return err
	}
	request := Request{ProtocolVersion: ProtocolVersion, ClientID: c.clientID, RequestID: requestID, Method: method, Generation: generation, Params: data}
	return c.call(ctx, request, result)
}

func (c *Client) request(ctx context.Context, method string, generation uint64, params any, result any) error {
	return c.requestWithID(ctx, randomID("req"), method, generation, params, result)
}

func (c *Client) AcquireRuntime(ctx context.Context, params AcquireParams) (RuntimeInfo, error) {
	return c.AcquireRuntimeWithRequestID(ctx, randomID("req"), params)
}

func (c *Client) AcquireRuntimeWithRequestID(ctx context.Context, requestID string, params AcquireParams) (RuntimeInfo, error) {
	for {
		var result RuntimeInfo
		err := c.requestWithID(ctx, requestID, "AcquireRuntime", 0, params, &result)
		if err == nil {
			return result, nil
		}
		coded, ok := err.(*SupervisorError)
		if !ok || coded.Code != CodeOperationPending {
			return RuntimeInfo{}, err
		}
		if status, statusErr := c.GetOperationStatus(ctx, "", requestID); statusErr == nil && status.State == "FAILED" {
			code := status.ErrorCode
			if code == "" {
				code = CodeStartFailed
			}
			return RuntimeInfo{}, &SupervisorError{Code: code, Message: status.ErrorMessage}
		}
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return RuntimeInfo{}, ctx.Err()
		case <-timer.C:
		}
	}
}
func (c *Client) HeartbeatLease(ctx context.Context, leaseID string, generation uint64) error {
	return c.request(ctx, "HeartbeatLease", generation, LeaseParams{LeaseID: leaseID}, nil)
}
func (c *Client) PinTurn(ctx context.Context, leaseID, sessionID string, generation uint64) error {
	return c.request(ctx, "PinTurn", generation, LeaseParams{LeaseID: leaseID, SessionID: sessionID}, nil)
}
func (c *Client) UnpinTurn(ctx context.Context, leaseID, sessionID string, generation uint64) error {
	return c.request(ctx, "UnpinTurn", generation, LeaseParams{LeaseID: leaseID, SessionID: sessionID}, nil)
}
func (c *Client) ReleaseRuntime(ctx context.Context, leaseID string, generation uint64) error {
	return c.request(ctx, "ReleaseRuntime", generation, LeaseParams{LeaseID: leaseID}, nil)
}
func (c *Client) GetRuntimeStatus(ctx context.Context, runtimeID string) (RuntimeStatus, error) {
	var result RuntimeStatus
	err := c.request(ctx, "GetRuntimeStatus", 0, LeaseParams{RuntimeID: runtimeID}, &result)
	return result, err
}

func (c *Client) GetOperationStatus(ctx context.Context, operationID, requestID string) (OperationStatus, error) {
	var result OperationStatus
	err := c.request(ctx, "GetOperationStatus", 0, OperationParams{OperationID: operationID, RequestID: requestID}, &result)
	return result, err
}

func (c *Client) StopRuntime(ctx context.Context, runtimeID string, generation uint64) error {
	return c.request(ctx, "StopRuntime", generation, StopParams{RuntimeID: runtimeID}, nil)
}
