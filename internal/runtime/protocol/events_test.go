package protocol

import (
	"encoding/json"
	"testing"
	"time"
)

func TestEventJSONRoundTrip(t *testing.T) {
	events := []Event{
		{Kind: EventAssistantDelta, Text: "hello"},
		{Kind: EventResponseReset},
		{
			Kind:          EventToolResult,
			TurnID:        "turn-1",
			ItemID:        "item-1",
			ParentID:      "parent-1",
			ApprovalID:    "approval-1",
			Decision:      "allow_session",
			DecisionScope: "session",
			ApprovalKeys:  []string{"approval-1"},
			WorkflowRunID: "run-1",
			Sequence:      42,
			StartedAt:     time.Unix(1700000000, 0).UTC(),
			ToolCallID:    "tc-1",
			ToolName:      "shell_run",
			Text:          "ok",
			Metadata:      map[string]any{"exit_code": float64(0)},
		},
		{
			Kind: EventApprovalRequired,
			Approval: &ApprovalRequest{
				SessionID: "s1",
				ToolCall:  ToolCall{ID: "tc-2", Name: "shell_run", Input: `{"cmd":"ls"}`},
				Reason:    "confirm command",
				Code:      "shell_command",
				Key:       "shell:ls",
			},
		},
		{
			Kind:          EventWorkflowSnapshot,
			WorkflowRunID: "run-2",
			Status:        "running",
			LocalResult: &LocalResult{
				Kind: "workflow",
				WorkflowPanelSnapshot: &WorkflowPanelSnapshot{
					RunID:        "run-2",
					Status:       "running",
					Summary:      "scan repo",
					CurrentPhase: "inspect",
				},
			},
		},
		{
			Kind:      EventSessionHydrated,
			SessionID: "s1",
			Messages:  []Message{{ID: "m1", Role: "assistant", Text: "done"}},
		},
	}

	for _, ev := range events {
		data, err := json.Marshal(ev)
		if err != nil {
			t.Fatalf("marshal %s: %v", ev.Kind, err)
		}
		var got Event
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatalf("unmarshal %s: %v", ev.Kind, err)
		}
		if got.Kind != ev.Kind {
			t.Fatalf("kind mismatch: got %q want %q", got.Kind, ev.Kind)
		}
	}
}

func TestRPCMessageJSONRoundTrip(t *testing.T) {
	request := RPCRequest{
		JSONRPC: JSONRPCVersion,
		ID:      json.RawMessage(`"req-1"`),
		Method:  RPCMethodTurnStart,
		Params:  json.RawMessage(`{"thread_id":"s1","input":[{"kind":"text","text":"hello"}]}`),
	}
	requestData, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	var gotRequest RPCRequest
	if err := json.Unmarshal(requestData, &gotRequest); err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}
	if gotRequest.JSONRPC != JSONRPCVersion || string(gotRequest.ID) != `"req-1"` || gotRequest.Method != RPCMethodTurnStart {
		t.Fatalf("unexpected request round trip: %+v", gotRequest)
	}

	notification := RPCNotification{
		JSONRPC: JSONRPCVersion,
		Method:  RPCMethodAppReady,
		Params:  json.RawMessage(`{"protocolVersion":"2","sessionId":"s1","workspaceRoot":"/tmp/work","capabilities":[]}`),
	}
	notificationData, err := json.Marshal(notification)
	if err != nil {
		t.Fatalf("marshal notification: %v", err)
	}
	var gotNotification RPCNotification
	if err := json.Unmarshal(notificationData, &gotNotification); err != nil {
		t.Fatalf("unmarshal notification: %v", err)
	}
	if gotNotification.JSONRPC != JSONRPCVersion || gotNotification.Method != RPCMethodAppReady {
		t.Fatalf("unexpected notification round trip: %+v", gotNotification)
	}

	success := RPCResponse{
		JSONRPC: JSONRPCVersion,
		ID:      json.RawMessage(`1`),
		Result:  json.RawMessage(`{"accepted":true}`),
	}
	successData, err := json.Marshal(success)
	if err != nil {
		t.Fatalf("marshal success response: %v", err)
	}
	var gotSuccess RPCResponse
	if err := json.Unmarshal(successData, &gotSuccess); err != nil {
		t.Fatalf("unmarshal success response: %v", err)
	}
	if gotSuccess.JSONRPC != JSONRPCVersion || string(gotSuccess.ID) != `1` || len(gotSuccess.Result) == 0 || gotSuccess.Error != nil {
		t.Fatalf("unexpected success response round trip: %+v", gotSuccess)
	}

	failure := RPCResponse{
		JSONRPC: JSONRPCVersion,
		ID:      json.RawMessage(`null`),
		Error:   &RPCErrorObject{Code: RPCErrorParseError, Message: "parse error"},
	}
	failureData, err := json.Marshal(failure)
	if err != nil {
		t.Fatalf("marshal error response: %v", err)
	}
	var gotFailure RPCResponse
	if err := json.Unmarshal(failureData, &gotFailure); err != nil {
		t.Fatalf("unmarshal error response: %v", err)
	}
	if gotFailure.JSONRPC != JSONRPCVersion || string(gotFailure.ID) != `null` || gotFailure.Error == nil || gotFailure.Error.Code != RPCErrorParseError {
		t.Fatalf("unexpected error response round trip: %+v", gotFailure)
	}
}

func TestThreadTurnJSONRoundTrip(t *testing.T) {
	thread := Thread{
		ID:        "s1",
		SessionID: "s1",
		Status:    ThreadStatusActive,
		CWD:       "/tmp/work",
		Model:     "deepseek-v3.2",
		Source:    "user",
		Turns: []Turn{{
			ID:       "turn-1",
			ThreadID: "s1",
			Status:   TurnStatusInProgress,
			Items: []ThreadItem{{
				ID:       "item-1",
				ThreadID: "s1",
				TurnID:   "turn-1",
				Type:     ThreadItemUserMessage,
				Role:     "user",
				Text:     "hello",
			}},
		}},
	}
	data, err := json.Marshal(thread)
	if err != nil {
		t.Fatalf("marshal thread: %v", err)
	}
	var got Thread
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal thread: %v", err)
	}
	if got.ID != thread.ID || got.Status != ThreadStatusActive || len(got.Turns) != 1 || len(got.Turns[0].Items) != 1 {
		t.Fatalf("unexpected thread round trip: %+v", got)
	}
	if got.Turns[0].Items[0].Type != ThreadItemUserMessage {
		t.Fatalf("thread item type = %q, want %q", got.Turns[0].Items[0].Type, ThreadItemUserMessage)
	}
}

func TestTypedItemNotificationsJSON(t *testing.T) {
	started := ItemStartedNotification{
		ThreadID: "s1",
		TurnID:   "turn-1",
		Item: ThreadItem{
			ID:         "tool_call:tc-1",
			ThreadID:   "s1",
			TurnID:     "turn-1",
			Type:       ThreadItemToolCall,
			ToolCallID: "tc-1",
			ToolName:   "shell",
			Input:      `{"cmd":"pwd"}`,
		},
		StartedAtMS: 1700000000000,
	}
	data, err := json.Marshal(started)
	if err != nil {
		t.Fatalf("marshal item started: %v", err)
	}
	if !json.Valid(data) || string(data) == "" {
		t.Fatalf("invalid item started json: %q", data)
	}
	var gotStarted ItemStartedNotification
	if err := json.Unmarshal(data, &gotStarted); err != nil {
		t.Fatalf("unmarshal item started: %v", err)
	}
	if gotStarted.Item.Type != ThreadItemToolCall || gotStarted.Item.Input == "" {
		t.Fatalf("unexpected item started round trip: %+v", gotStarted)
	}

	delta := ItemDeltaNotification{
		ThreadID: "s1",
		TurnID:   "turn-1",
		ItemID:   "agent_message:turn-1",
		Delta: ThreadItemDelta{
			ItemID: "agent_message:turn-1",
			Type:   ThreadItemDeltaAgentMessage,
			Text:   "hello",
		},
	}
	data, err = json.Marshal(delta)
	if err != nil {
		t.Fatalf("marshal item delta: %v", err)
	}
	var gotDelta ItemDeltaNotification
	if err := json.Unmarshal(data, &gotDelta); err != nil {
		t.Fatalf("unmarshal item delta: %v", err)
	}
	if gotDelta.Delta.Type != ThreadItemDeltaAgentMessage || gotDelta.Delta.ItemID != delta.ItemID {
		t.Fatalf("unexpected item delta round trip: %+v", gotDelta)
	}
}
