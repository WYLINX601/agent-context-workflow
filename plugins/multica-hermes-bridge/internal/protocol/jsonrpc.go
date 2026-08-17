package protocol

import (
	"bytes"
	"encoding/json"
	"fmt"
)

type Message struct {
	JSONRPC string          `json:"jsonrpc,omitempty"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *Error          `json:"error,omitempty"`
}

type Error struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if len(e.Data) == 0 {
		return fmt.Sprintf("json-rpc error %d: %s", e.Code, e.Message)
	}
	return fmt.Sprintf("json-rpc error %d: %s (%s)", e.Code, e.Message, string(e.Data))
}

func NewRequest(id any, method string, params any) (Message, error) {
	rawID, err := json.Marshal(id)
	if err != nil {
		return Message{}, err
	}
	rawParams, err := json.Marshal(params)
	if err != nil {
		return Message{}, err
	}
	return Message{JSONRPC: "2.0", ID: rawID, Method: method, Params: rawParams}, nil
}

func NewNotification(method string, params any) (Message, error) {
	rawParams, err := json.Marshal(params)
	if err != nil {
		return Message{}, err
	}
	return Message{JSONRPC: "2.0", Method: method, Params: rawParams}, nil
}

func NewResult(id json.RawMessage, result any) (Message, error) {
	rawResult, err := json.Marshal(result)
	if err != nil {
		return Message{}, err
	}
	return Message{JSONRPC: "2.0", ID: append(json.RawMessage(nil), id...), Result: rawResult}, nil
}

func NewError(id json.RawMessage, rpcError *Error) Message {
	return Message{JSONRPC: "2.0", ID: append(json.RawMessage(nil), id...), Error: rpcError}
}

func IsNotification(m Message) bool {
	return m.Method != "" && len(bytes.TrimSpace(m.ID)) == 0
}

func IDKey(id json.RawMessage) string {
	return string(bytes.TrimSpace(id))
}

func DecodeParams(raw json.RawMessage, target any) error {
	if len(bytes.TrimSpace(raw)) == 0 {
		raw = []byte(`{}`)
	}
	if err := json.Unmarshal(raw, target); err != nil {
		return fmt.Errorf("invalid params: %w", err)
	}
	return nil
}

func RawMap(raw json.RawMessage) map[string]any {
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil || value == nil {
		return map[string]any{}
	}
	return value
}
