package appserver

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/usewhale/whale/internal/app"
	"github.com/usewhale/whale/internal/runtime/protocol"
)

func TestRunEmitsReadyAndClosedOnEOF(t *testing.T) {
	msgs := decodeRPCMessages(t, runAppServerTest(t, ""))
	if len(msgs) < 2 {
		t.Fatalf("expected ready and closed messages, got %+v", msgs)
	}
	if got := notificationMethod(msgs[0]); got != protocol.RPCMethodAppReady {
		t.Fatalf("first method = %q, want %q", got, protocol.RPCMethodAppReady)
	}
	if got := notificationMethod(msgs[len(msgs)-1]); got != protocol.RPCMethodAppClosed {
		t.Fatalf("last method = %q, want %q", got, protocol.RPCMethodAppClosed)
	}
}

func TestRunInitializesOnRequest(t *testing.T) {
	input := encodeRPCRequest(t, 1, protocol.RPCMethodAppInitialize, nil) +
		encodeRPCRequest(t, 2, protocol.RPCMethodAppShutdown, nil)
	msgs := decodeRPCMessages(t, runAppServerTest(t, input))
	resp := findResponse(t, msgs, float64(1))
	if resp.Error != nil {
		t.Fatalf("initialize returned error: %+v", resp.Error)
	}
	var info protocol.AppInfo
	decodeRaw(t, resp.Result, &info)
	if info.ProtocolVersion != "2" || info.SessionID == "" || info.WorkspaceRoot == "" {
		t.Fatalf("unexpected app info: %+v", info)
	}
	if contains(info.Capabilities, "intent/dispatch") {
		t.Fatalf("capabilities still expose legacy intent dispatch: %+v", info.Capabilities)
	}
	if !contains(info.Capabilities, protocol.RPCMethodThreadCurrent) || !contains(info.Capabilities, protocol.RPCMethodTurnStart) {
		t.Fatalf("capabilities missing thread/turn methods: %+v", info.Capabilities)
	}
	if contains(info.Capabilities, "item/published") {
		t.Fatalf("capabilities still expose legacy item/published: %+v", info.Capabilities)
	}
	if !contains(info.Capabilities, protocol.RPCMethodItemStarted) ||
		!contains(info.Capabilities, protocol.RPCMethodItemDelta) ||
		!contains(info.Capabilities, protocol.RPCMethodItemCompleted) {
		t.Fatalf("capabilities missing typed item lifecycle methods: %+v", info.Capabilities)
	}
}

func TestRunReportsInvalidJSONAndContinues(t *testing.T) {
	input := "{bad}\n" + encodeRPCRequest(t, 1, protocol.RPCMethodAppShutdown, nil)
	msgs := decodeRPCMessages(t, runAppServerTest(t, input))
	if !hasRPCError(msgs, protocol.RPCErrorParseError) {
		t.Fatalf("expected parse error, got %+v", msgs)
	}
	if got := notificationMethod(msgs[len(msgs)-1]); got != protocol.RPCMethodAppClosed {
		t.Fatalf("server did not continue to shutdown, got %+v", msgs)
	}
}

func TestRunShutdownReturnsSuccessAndCloses(t *testing.T) {
	msgs := decodeRPCMessages(t, runAppServerTest(t, encodeRPCRequest(t, "shutdown", protocol.RPCMethodAppShutdown, nil)))
	resp := findResponse(t, msgs, "shutdown")
	if resp.Error != nil {
		t.Fatalf("shutdown returned error: %+v", resp.Error)
	}
	var accepted protocol.AcceptedResponse
	decodeRaw(t, resp.Result, &accepted)
	if !accepted.Accepted {
		t.Fatalf("shutdown response = %+v, want accepted", accepted)
	}
	if got := notificationMethod(msgs[len(msgs)-1]); got != protocol.RPCMethodAppClosed {
		t.Fatalf("shutdown did not emit closed notification, got %+v", msgs)
	}
}

func TestRunReturnsCurrentThread(t *testing.T) {
	input := encodeRPCRequest(t, "current", protocol.RPCMethodThreadCurrent, nil) +
		encodeRPCRequest(t, "shutdown", protocol.RPCMethodAppShutdown, nil)
	msgs := decodeRPCMessages(t, runAppServerTest(t, input))
	resp := findResponse(t, msgs, "current")
	if resp.Error != nil {
		t.Fatalf("thread/current returned error: %+v", resp.Error)
	}
	var out protocol.ThreadCurrentResponse
	decodeRaw(t, resp.Result, &out)
	if out.Thread.ID == "" || out.Thread.SessionID != out.Thread.ID || out.Thread.Status != protocol.ThreadStatusIdle {
		t.Fatalf("unexpected current thread: %+v", out.Thread)
	}
}

func TestRunAcceptsLargeProtocolFrame(t *testing.T) {
	input := encodeRPCRequest(t, 1, protocol.RPCMethodThreadList, map[string]any{
		"limit":   10,
		"padding": strings.Repeat("x", 70*1024),
	}) + encodeRPCRequest(t, 2, protocol.RPCMethodAppShutdown, nil)
	msgs := decodeRPCMessages(t, runAppServerTest(t, input))
	if hasRPCError(msgs, protocol.RPCErrorInternal) {
		t.Fatalf("large valid client frame should not produce read error, got %+v", msgs)
	}
	if findResponse(t, msgs, float64(1)).Error != nil {
		t.Fatalf("large thread/list returned error: %+v", msgs)
	}
	if got := notificationMethod(msgs[len(msgs)-1]); got != protocol.RPCMethodAppClosed {
		t.Fatalf("server did not close cleanly after large frame, got %+v", msgs)
	}
}

func TestRunStartsAndResumesThread(t *testing.T) {
	input := encodeRPCRequest(t, "current", protocol.RPCMethodThreadCurrent, nil) +
		encodeRPCRequest(t, "start", protocol.RPCMethodThreadStart, nil) +
		encodeRPCRequest(t, "list", protocol.RPCMethodThreadList, protocol.ThreadListParams{Limit: 10}) +
		encodeRPCRequest(t, "shutdown", protocol.RPCMethodAppShutdown, nil)
	msgs := decodeRPCMessages(t, runAppServerTest(t, input))
	var current protocol.ThreadCurrentResponse
	decodeRaw(t, findResponse(t, msgs, "current").Result, &current)
	var started protocol.ThreadStartResponse
	decodeRaw(t, findResponse(t, msgs, "start").Result, &started)
	if started.Thread.ID == "" || started.Thread.ID == current.Thread.ID {
		t.Fatalf("thread/start did not create a new thread: current=%+v started=%+v", current.Thread, started.Thread)
	}
	if !hasNotification(msgs, protocol.RPCMethodThreadStarted) {
		t.Fatalf("thread/start did not emit thread/started: %+v", msgs)
	}
	var listed protocol.ThreadListResponse
	decodeRaw(t, findResponse(t, msgs, "list").Result, &listed)
	if len(listed.Threads) < 1 || listed.Threads[0].ID != started.Thread.ID {
		t.Fatalf("thread/list did not include current started thread: %+v", listed.Threads)
	}
}

func TestRunRejectsUnknownMethod(t *testing.T) {
	input := encodeRPCRequest(t, "missing", "missing/method", nil) +
		encodeRPCRequest(t, "shutdown", protocol.RPCMethodAppShutdown, nil)
	msgs := decodeRPCMessages(t, runAppServerTest(t, input))
	resp := findResponse(t, msgs, "missing")
	if resp.Error == nil || resp.Error.Code != protocol.RPCErrorMethodNotFound {
		t.Fatalf("expected method-not-found error, got %+v", resp)
	}
}

func TestRunRejectsLegacyFrame(t *testing.T) {
	input := `{"type":"intent","intent":{"kind":"shutdown"}}` + "\n" +
		encodeRPCRequest(t, "shutdown", protocol.RPCMethodAppShutdown, nil)
	msgs := decodeRPCMessages(t, runAppServerTest(t, input))
	if !hasRPCError(msgs, protocol.RPCErrorInvalidRequest) {
		t.Fatalf("expected invalid request for legacy frame, got %+v", msgs)
	}
}

func TestRunRejectsInvalidParams(t *testing.T) {
	input := encodeRPCRequest(t, 1, protocol.RPCMethodTurnStart, protocol.TurnStartParams{
		Input: []protocol.UserInput{{Kind: protocol.UserInputImage, Text: "unsupported"}},
	}) +
		encodeRPCRequest(t, 2, protocol.RPCMethodAppShutdown, nil)
	msgs := decodeRPCMessages(t, runAppServerTest(t, input))
	resp := findResponse(t, msgs, float64(1))
	if resp.Error == nil || resp.Error.Code != protocol.RPCErrorInvalidParams {
		t.Fatalf("expected invalid params error, got %+v", resp)
	}
}

func TestWriteServiceMessageNotificationUsesTypedItemLifecycle(t *testing.T) {
	messages := collectWrittenMessages(t, []protocol.ServiceMessage{
		{
			Type:     protocol.ServiceMessageItemDelta,
			ThreadID: "s1",
			TurnID:   "turn-1",
			Delta: &protocol.ThreadItemDelta{
				ItemID: "agent_message:turn-1",
				Type:   protocol.ThreadItemDeltaAgentMessage,
				Text:   "hello",
			},
		},
		{
			Type:     protocol.ServiceMessageItemStarted,
			ThreadID: "s1",
			TurnID:   "turn-1",
			Item: &protocol.ThreadItem{
				ID:         "tool_call:tc-1",
				ThreadID:   "s1",
				TurnID:     "turn-1",
				Type:       protocol.ThreadItemToolCall,
				ToolCallID: "tc-1",
				ToolName:   "shell",
				Input:      `{"cmd":"pwd"}`,
			},
		},
		{
			Type:     protocol.ServiceMessageItemCompleted,
			ThreadID: "s1",
			TurnID:   "turn-1",
			Item: &protocol.ThreadItem{
				ID:         "tool_result:tc-1",
				ThreadID:   "s1",
				TurnID:     "turn-1",
				Type:       protocol.ThreadItemToolResult,
				ToolCallID: "tc-1",
				ToolName:   "shell",
				Output:     "ok",
			},
		},
	})
	if got, want := notificationMethod(messages[0]), protocol.RPCMethodItemDelta; got != want {
		t.Fatalf("message 0 method = %q, want %q", got, want)
	}
	if got, want := notificationMethod(messages[1]), protocol.RPCMethodItemStarted; got != want {
		t.Fatalf("message 1 method = %q, want %q", got, want)
	}
	if got, want := notificationMethod(messages[2]), protocol.RPCMethodItemCompleted; got != want {
		t.Fatalf("message 2 method = %q, want %q", got, want)
	}
}

func collectWrittenMessages(t *testing.T, serviceMessages []protocol.ServiceMessage) []rpcMessage {
	t.Helper()
	var out bytes.Buffer
	enc := json.NewEncoder(&out)
	write := func(msg any) error {
		return enc.Encode(msg)
	}
	for _, msg := range serviceMessages {
		if err := writeServiceMessageNotification(write, msg); err != nil {
			t.Fatalf("write service message notification: %v", err)
		}
	}
	return decodeRPCMessages(t, out.String())
}

func runAppServerTest(t *testing.T, input string) string {
	t.Helper()
	t.Setenv("DEEPSEEK_API_KEY", "sk-test")
	t.Setenv("HOME", t.TempDir())
	workspace := t.TempDir()
	t.Chdir(workspace)

	cfg := app.DefaultConfig()
	cfg.DataDir = t.TempDir()
	var out bytes.Buffer
	if err := Run(context.Background(), cfg, app.StartOptions{NewSession: true}, strings.NewReader(input), &out); err != nil {
		t.Fatalf("Run: %v", err)
	}
	return out.String()
}

func encodeRPCRequest(t *testing.T, id any, method string, params any) string {
	t.Helper()
	msg := map[string]any{
		"jsonrpc": protocol.JSONRPCVersion,
		"id":      id,
		"method":  method,
	}
	if params != nil {
		msg["params"] = params
	}
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal rpc request: %v", err)
	}
	return string(data) + "\n"
}

type rpcMessage struct {
	JSONRPC string                   `json:"jsonrpc"`
	ID      json.RawMessage          `json:"id"`
	Method  string                   `json:"method"`
	Params  json.RawMessage          `json:"params"`
	Result  json.RawMessage          `json:"result"`
	Error   *protocol.RPCErrorObject `json:"error"`
}

func decodeRPCMessages(t *testing.T, raw string) []rpcMessage {
	t.Helper()
	var msgs []rpcMessage
	scanner := bufio.NewScanner(strings.NewReader(raw))
	scanner.Buffer(make([]byte, 0, 64*1024), maxClientMessageBytes)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		var msg rpcMessage
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			t.Fatalf("unmarshal rpc message %q: %v\nall output:\n%s", line, err, raw)
		}
		msgs = append(msgs, msg)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan rpc messages: %v", err)
	}
	return msgs
}

func notificationMethod(msg rpcMessage) string {
	return msg.Method
}

func findResponse(t *testing.T, msgs []rpcMessage, id any) rpcMessage {
	t.Helper()
	want, err := json.Marshal(id)
	if err != nil {
		t.Fatalf("marshal id: %v", err)
	}
	for _, msg := range msgs {
		if len(msg.ID) > 0 && bytes.Equal(msg.ID, want) {
			return msg
		}
	}
	t.Fatalf("response id %s not found in %+v", want, msgs)
	return rpcMessage{}
}

func hasRPCError(msgs []rpcMessage, code int) bool {
	for _, msg := range msgs {
		if msg.Error != nil && msg.Error.Code == code {
			return true
		}
	}
	return false
}

func hasNotification(msgs []rpcMessage, method string) bool {
	for _, msg := range msgs {
		if notificationMethod(msg) == method {
			return true
		}
	}
	return false
}

func decodeRaw(t *testing.T, raw json.RawMessage, out any) {
	if t != nil {
		t.Helper()
	}
	if err := json.Unmarshal(raw, out); err != nil {
		if t == nil {
			panic(err)
		}
		t.Fatalf("decode raw: %v", err)
	}
}

func contains(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
