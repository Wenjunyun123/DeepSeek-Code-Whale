package tui

import (
	"strings"
	"testing"

	"github.com/usewhale/whale/internal/runtime/protocol"
	tuirender "github.com/usewhale/whale/internal/tui/render"
)

func TestServiceMessageDeltaRendersAssistantText(t *testing.T) {
	m := newModel(nil, "", "", "")
	cmd, quit, direct := m.handleServiceMessages([]protocol.ServiceMessage{{
		Type:     protocol.ServiceMessageItemDelta,
		ThreadID: "s1",
		TurnID:   "turn-1",
		Delta: &protocol.ThreadItemDelta{
			ItemID: "agent_message:turn-1",
			Type:   protocol.ThreadItemDeltaAgentMessage,
			Text:   "typed answer",
		},
	}})
	if quit || direct {
		t.Fatalf("unexpected service message result: cmd=%v quit=%v direct=%v", cmd, quit, direct)
	}
	rendered := strings.Join(tuirender.ChatLines(m.chatMessages(), 80), "\n")
	if !strings.Contains(rendered, "typed answer") {
		t.Fatalf("view missing typed answer:\n%s", rendered)
	}
}

func TestServiceMessagePlanUpdatePreservesPlanUpdateBehavior(t *testing.T) {
	m := newModel(nil, "", "", "")
	_, quit, direct := m.handleServiceMessages([]protocol.ServiceMessage{{
		Type:     protocol.ServiceMessageItemCompleted,
		ThreadID: "s1",
		TurnID:   "turn-1",
		Item: &protocol.ThreadItem{
			ID:       "plan:turn-1",
			ThreadID: "s1",
			TurnID:   "turn-1",
			Type:     protocol.ThreadItemPlan,
			Text:     "[x] Inspect\n[ ] Test",
			Status:   "update",
		},
	}})
	if quit || direct {
		t.Fatalf("unexpected plan update result: quit=%v direct=%v", quit, direct)
	}
	rendered := strings.Join(tuirender.ChatLines(m.chatMessages(), 80), "\n")
	if !strings.Contains(rendered, "Updated Plan") || !strings.Contains(rendered, "Inspect") || !strings.Contains(rendered, "Test") {
		t.Fatalf("plan update did not render:\n%s", rendered)
	}
	if m.sawPlanThisTurn {
		t.Fatalf("plan update should not mark final proposed plan")
	}
}

func TestServiceMessageControlPreservesPendingInputState(t *testing.T) {
	m := newModel(nil, "", "", "")
	m.pendingSteers = []pendingSteer{{ID: "pending-1", Text: "later"}}
	_, quit, direct := m.handleServiceMessages([]protocol.ServiceMessage{{
		Type:     protocol.ServiceMessageControl,
		ThreadID: "s1",
		Control: &protocol.ControlMessage{
			Type:          protocol.ControlMessagePendingInputAccepted,
			ClientInputID: "pending-1",
		},
	}})
	if quit || direct {
		t.Fatalf("unexpected pending input result: quit=%v direct=%v", quit, direct)
	}
	if len(m.pendingSteers) != 1 || !m.pendingSteers[0].Accepted {
		t.Fatalf("pending steer not accepted: %+v", m.pendingSteers)
	}
}
