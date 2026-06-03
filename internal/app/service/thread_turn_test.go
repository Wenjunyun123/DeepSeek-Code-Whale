package service

import (
	"testing"
	"time"

	"github.com/usewhale/whale/internal/runtime/protocol"
)

func TestEventServiceMessagesMapsTimelineLifecycle(t *testing.T) {
	threadID := "s1"
	active := protocol.Turn{
		ID:        "turn-1",
		ThreadID:  threadID,
		Status:    protocol.TurnStatusInProgress,
		StartedAt: time.Unix(1700000000, 0).UTC(),
	}

	deltaMessages := EventServiceMessages(threadID, Event{
		Kind:   EventAssistantDelta,
		TurnID: "turn-1",
		Text:   "hello",
	}, active)
	if len(deltaMessages) != 1 || deltaMessages[0].Type != protocol.ServiceMessageItemDelta {
		t.Fatalf("assistant delta messages = %+v", deltaMessages)
	}
	if deltaMessages[0].Delta == nil || deltaMessages[0].Delta.Type != protocol.ThreadItemDeltaAgentMessage {
		t.Fatalf("assistant delta payload = %+v", deltaMessages[0].Delta)
	}

	startMessages := EventServiceMessages(threadID, Event{
		Kind:       EventToolCall,
		TurnID:     "turn-1",
		ToolCallID: "tc-1",
		ToolName:   "shell",
		Text:       `{"cmd":"pwd"}`,
	}, active)
	if len(startMessages) != 1 || startMessages[0].Type != protocol.ServiceMessageItemStarted {
		t.Fatalf("tool call messages = %+v", startMessages)
	}
	if startMessages[0].Item == nil || startMessages[0].Item.Type != protocol.ThreadItemToolCall || startMessages[0].Item.Input == "" {
		t.Fatalf("tool call item = %+v", startMessages[0].Item)
	}

	doneMessages := EventServiceMessages(threadID, Event{
		Kind:   EventTurnDone,
		TurnID: "turn-1",
	}, active)
	if len(doneMessages) != 2 {
		t.Fatalf("turn done messages = %+v", doneMessages)
	}
	if doneMessages[0].Type != protocol.ServiceMessageTurnCompleted || doneMessages[1].Type != protocol.ServiceMessageThreadStatusChanged {
		t.Fatalf("unexpected turn done message types = %+v", doneMessages)
	}
}

func TestEventServiceMessagesKeepsControlOffTimeline(t *testing.T) {
	messages := EventServiceMessages("s1", Event{
		Kind: EventApprovalRequired,
		Approval: &protocol.ApprovalRequest{
			SessionID: "s1",
			ToolCall:  protocol.ToolCall{ID: "tc-1", Name: "shell"},
			Reason:    "confirm",
		},
	}, protocol.Turn{})
	if len(messages) != 1 || messages[0].Type != protocol.ServiceMessageControl {
		t.Fatalf("approval messages = %+v", messages)
	}
	if messages[0].Control == nil || messages[0].Control.Type != protocol.ControlMessageApprovalRequired {
		t.Fatalf("approval control = %+v", messages[0].Control)
	}
	if messages[0].Item != nil || messages[0].Delta != nil {
		t.Fatalf("control message leaked into timeline: %+v", messages[0])
	}
}
