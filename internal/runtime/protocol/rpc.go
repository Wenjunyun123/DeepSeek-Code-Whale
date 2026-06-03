package protocol

import "encoding/json"

const JSONRPCVersion = "2.0"

const (
	RPCMethodAppInitialize       = "app/initialize"
	RPCMethodAppReady            = "app/ready"
	RPCMethodAppShutdown         = "app/shutdown"
	RPCMethodAppClosed           = "app/closed"
	RPCMethodThreadCurrent       = "thread/current"
	RPCMethodThreadList          = "thread/list"
	RPCMethodThreadRead          = "thread/read"
	RPCMethodThreadStart         = "thread/start"
	RPCMethodThreadResume        = "thread/resume"
	RPCMethodThreadStarted       = "thread/started"
	RPCMethodThreadStatusChanged = "thread/status/changed"
	RPCMethodTurnStart           = "turn/start"
	RPCMethodTurnInterrupt       = "turn/interrupt"
	RPCMethodTurnStarted         = "turn/started"
	RPCMethodTurnCompleted       = "turn/completed"
	RPCMethodItemStarted         = "item/started"
	RPCMethodItemDelta           = "item/delta"
	RPCMethodItemCompleted       = "item/completed"
)

const (
	RPCErrorParseError     = -32700
	RPCErrorInvalidRequest = -32600
	RPCErrorMethodNotFound = -32601
	RPCErrorInvalidParams  = -32602
	RPCErrorInternal       = -32603
)

type RPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type RPCNotification struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type RPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCErrorObject `json:"error,omitempty"`
}

type RPCErrorObject struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

type AppInfo struct {
	ProtocolVersion string   `json:"protocolVersion"`
	SessionID       string   `json:"sessionId"`
	WorkspaceRoot   string   `json:"workspaceRoot"`
	Capabilities    []string `json:"capabilities"`
}

type AcceptedResponse struct {
	Accepted bool `json:"accepted"`
}

type ClosedNotification struct {
	Reason string `json:"reason"`
}
