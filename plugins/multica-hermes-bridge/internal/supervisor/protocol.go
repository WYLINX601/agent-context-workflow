package supervisor

import (
	"encoding/json"
	"errors"
)

const ProtocolVersion = 1

const (
	CodeUnavailable        = "MHG1101 SUPERVISOR_UNAVAILABLE"
	CodeStartFailed        = "MHG1102 RUNTIME_START_FAILED"
	CodeStartTimeout       = "MHG1103 RUNTIME_START_TIMEOUT"
	CodeGenerationChanged  = "MHG1104 RUNTIME_GENERATION_MISMATCH"
	CodeRuntimeUnavailable = "MHG1105 RUNTIME_UNAVAILABLE"
	CodeOperationPending   = "MHG1106 OPERATION_PENDING"
	CodeLeaseExpired       = "MHG1107 LEASE_EXPIRED"
	CodeSharedMultiplex    = "MHG3006 SHARED_MULTIPLEX_REQUIRED"
	CodeProfileNotServed   = "MHG3007 PROFILE_NOT_SERVED"
	CodeProfileNotFound    = "MHG3004 PROFILE_NOT_FOUND"
	CodeProfileLookup      = "MHG3005 PROFILE_LOOKUP_FAILED"
)

type Request struct {
	ProtocolVersion int             `json:"protocol_version"`
	ClientID        string          `json:"client_id"`
	RequestID       string          `json:"request_id"`
	Method          string          `json:"method"`
	Generation      uint64          `json:"generation,omitempty"`
	Params          json.RawMessage `json:"params,omitempty"`
}

type Response struct {
	ProtocolVersion int            `json:"protocol_version"`
	RequestID       string         `json:"request_id"`
	OK              bool           `json:"ok"`
	Result          any            `json:"result,omitempty"`
	Error           *ResponseError `json:"error,omitempty"`
}

type ResponseError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type AcquireParams struct {
	GatewayIdentity string `json:"gateway_identity"`
	Profile         string `json:"profile"`
	Scope           string `json:"scope"`
}

type LeaseParams struct {
	LeaseID   string `json:"lease_id"`
	SessionID string `json:"session_id,omitempty"`
	RuntimeID string `json:"runtime_id,omitempty"`
}

type StopParams struct {
	RuntimeID string `json:"runtime_id"`
}

type OperationParams struct {
	OperationID string `json:"operation_id,omitempty"`
	RequestID   string `json:"request_id,omitempty"`
}

type RuntimeInfo struct {
	RuntimeID   string `json:"runtime_id"`
	LeaseID     string `json:"lease_id"`
	Generation  uint64 `json:"generation"`
	Ownership   string `json:"ownership"`
	GatewayURL  string `json:"gateway_url"`
	StatusURL   string `json:"status_url"`
	TokenRef    string `json:"token_ref,omitempty"`
	OperationID string `json:"operation_id,omitempty"`
}

type RuntimeStatus struct {
	RuntimeInfo
	State       string `json:"state"`
	PID         int    `json:"pid,omitempty"`
	LeaseCount  int    `json:"lease_count"`
	PinCount    int    `json:"pin_count"`
	IdentityOK  bool   `json:"identity_ok"`
	LastHealthy string `json:"last_healthy,omitempty"`
}

type OperationStatus struct {
	OperationID       string `json:"operation_id"`
	ClientID          string `json:"client_id,omitempty"`
	RequestID         string `json:"request_id,omitempty"`
	RuntimeID         string `json:"runtime_id,omitempty"`
	State             string `json:"state"`
	PID               int    `json:"pid,omitempty"`
	ProcessStartToken string `json:"process_start_token,omitempty"`
	Endpoint          string `json:"endpoint,omitempty"`
	LaunchNonce       string `json:"launch_nonce,omitempty"`
	ErrorCode         string `json:"error_code,omitempty"`
	ErrorMessage      string `json:"error_message,omitempty"`
	UpdatedAt         string `json:"updated_at,omitempty"`
}

type SupervisorError struct {
	Code    string
	Message string
}

func (e *SupervisorError) Error() string {
	if e == nil {
		return ""
	}
	return e.Code + ": " + e.Message
}

func errorResponse(requestID string, err error) Response {
	var coded *SupervisorError
	if errors.As(err, &coded) {
		return Response{ProtocolVersion: ProtocolVersion, RequestID: requestID, Error: &ResponseError{Code: coded.Code, Message: coded.Message}}
	}
	return Response{ProtocolVersion: ProtocolVersion, RequestID: requestID, Error: &ResponseError{Code: CodeRuntimeUnavailable, Message: err.Error()}}
}
