package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/usewhale/whale/internal/agent"
	"github.com/usewhale/whale/internal/core"
	"github.com/usewhale/whale/internal/runtime/protocol"
	"github.com/usewhale/whale/internal/session"
)

func (s *Service) CurrentThread() (protocol.Thread, error) {
	return s.threadForSession(s.app.SessionID(), nil, false)
}

func (s *Service) ListThreads(limit int) ([]protocol.Thread, error) {
	summaries, err := s.app.ListWorkspaceSessionSummaries(limit)
	if err != nil {
		return nil, err
	}
	out := make([]protocol.Thread, 0, len(summaries))
	hasCurrent := false
	for _, summary := range summaries {
		if summary.ID == s.app.SessionID() {
			hasCurrent = true
		}
		out = append(out, s.threadFromSummary(summary, nil, false))
	}
	if !hasCurrent {
		current, err := s.CurrentThread()
		if err != nil {
			return nil, err
		}
		out = append([]protocol.Thread{current}, out...)
	}
	return out, nil
}

func (s *Service) ReadThread(threadID string, includeTurns bool) (protocol.Thread, error) {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return protocol.Thread{}, errors.New("thread_id is required")
	}
	msgs, err := s.app.ListSessionMessages(threadID)
	if err != nil {
		return protocol.Thread{}, err
	}
	return s.threadForSession(threadID, msgs, includeTurns)
}

func (s *Service) StartThread() (protocol.Thread, error) {
	if err := s.ensureNoActiveTurn(); err != nil {
		return protocol.Thread{}, err
	}
	if _, err := s.app.StartNewSession(); err != nil {
		return protocol.Thread{}, err
	}
	return s.CurrentThread()
}

func (s *Service) ResumeThread(threadID string) (protocol.Thread, error) {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return protocol.Thread{}, errors.New("thread_id is required")
	}
	if err := s.ensureNoActiveTurn(); err != nil {
		return protocol.Thread{}, err
	}
	res, err := s.app.ApplyResumeChoice(threadID)
	if err != nil {
		return protocol.Thread{}, err
	}
	if !res.Resumed {
		return protocol.Thread{}, errors.New(res.Message)
	}
	return s.CurrentThread()
}

func (s *Service) StartTurn(params protocol.TurnStartParams) (protocol.Turn, error) {
	threadID := strings.TrimSpace(params.ThreadID)
	if threadID == "" {
		threadID = s.app.SessionID()
	}
	if threadID != s.app.SessionID() {
		return protocol.Turn{}, fmt.Errorf("thread %q is not loaded", threadID)
	}
	if err := s.ensureNoActiveTurn(); err != nil {
		return protocol.Turn{}, err
	}
	line, err := turnInputText(params.Input)
	if err != nil {
		return protocol.Turn{}, err
	}
	now := time.Now()
	turn := protocol.Turn{
		ID:        s.nextTurnID(),
		ThreadID:  threadID,
		Status:    protocol.TurnStatusInProgress,
		StartedAt: now,
	}
	s.goTracked(func() {
		s.runTurnWithID(turn.ID, now, func(ctx context.Context) (<-chan agent.AgentEvent, error) {
			return s.app.RunTurnWithOptions(ctx, line, s.tuiRunOptions(agent.RunOptions{}))
		})
	})
	return turn, nil
}

func (s *Service) InterruptTurn(params protocol.TurnInterruptParams) (protocol.TurnInterruptResponse, error) {
	threadID := strings.TrimSpace(params.ThreadID)
	if threadID == "" {
		return protocol.TurnInterruptResponse{}, errors.New("thread_id is required")
	}
	if threadID != s.app.SessionID() {
		return protocol.TurnInterruptResponse{}, fmt.Errorf("thread %q is not loaded", threadID)
	}
	turnID := strings.TrimSpace(params.TurnID)
	if turnID == "" {
		return protocol.TurnInterruptResponse{}, errors.New("turn_id is required")
	}
	s.cancelMu.Lock()
	defer s.cancelMu.Unlock()
	if !s.active || s.activeTurnID != turnID || s.cancel == nil {
		return protocol.TurnInterruptResponse{}, fmt.Errorf("turn %q is not active", turnID)
	}
	s.cancel()
	return protocol.TurnInterruptResponse{Interrupted: true}, nil
}

func (s *Service) ActiveTurnSnapshot() (protocol.Turn, bool) {
	s.cancelMu.Lock()
	defer s.cancelMu.Unlock()
	if !s.active || s.activeTurnID == "" {
		return protocol.Turn{}, false
	}
	return protocol.Turn{
		ID:        s.activeTurnID,
		ThreadID:  s.app.SessionID(),
		Status:    protocol.TurnStatusInProgress,
		StartedAt: s.activeTurnStart,
	}, true
}

func (s *Service) nextTurnID() string {
	return fmt.Sprintf("turn-%d", s.nextTurnSequence.Add(1))
}

func (s *Service) ensureNoActiveTurn() error {
	s.cancelMu.Lock()
	defer s.cancelMu.Unlock()
	if s.active {
		return errors.New("session busy")
	}
	return nil
}

func turnInputText(inputs []protocol.UserInput) (string, error) {
	var parts []string
	for _, input := range inputs {
		switch input.Kind {
		case "", protocol.UserInputText:
			text := strings.TrimSpace(input.Text)
			if text != "" {
				parts = append(parts, text)
			}
		default:
			return "", fmt.Errorf("unsupported input kind: %s", input.Kind)
		}
	}
	line := strings.TrimSpace(strings.Join(parts, "\n\n"))
	if line == "" {
		return "", errors.New("text input is required")
	}
	return line, nil
}

func (s *Service) threadForSession(sessionID string, msgs []core.Message, includeTurns bool) (protocol.Thread, error) {
	meta, err := session.LoadSessionMeta(s.app.SessionsDir(), sessionID)
	if err != nil {
		return protocol.Thread{}, err
	}
	thread := s.threadFromSummary(session.SessionSummary{ID: sessionID, Meta: meta}, msgs, includeTurns)
	if includeTurns && msgs == nil {
		msgs, err = s.app.ListSessionMessages(sessionID)
		if err != nil {
			return protocol.Thread{}, err
		}
		thread.Turns = turnsFromMessages(sessionID, msgs)
	}
	return thread, nil
}

func (s *Service) threadFromSummary(summary session.SessionSummary, msgs []core.Message, includeTurns bool) protocol.Thread {
	status := protocol.ThreadStatusNotLoaded
	if summary.ID == s.app.SessionID() {
		status = protocol.ThreadStatusIdle
		if _, ok := s.ActiveTurnSnapshot(); ok {
			status = protocol.ThreadStatusActive
		}
	}
	title := strings.TrimSpace(summary.Meta.Title)
	if title == "" {
		title = strings.TrimSpace(summary.Conversation)
	}
	thread := protocol.Thread{
		ID:        summary.ID,
		SessionID: summary.ID,
		Status:    status,
		CWD:       firstNonEmpty(summary.Meta.Workspace, s.app.WorkspaceRoot()),
		Model:     firstNonEmpty(summary.Meta.Model, s.app.Model()),
		Source:    firstNonEmpty(summary.Meta.Kind, "user"),
		Title:     title,
		Summary:   summary.Meta.Summary,
		CreatedAt: summary.Meta.StartedAt,
		UpdatedAt: firstNonZeroTime(summary.Meta.UpdatedAt, summary.ModTime),
	}
	if includeTurns {
		thread.Turns = turnsFromMessages(summary.ID, msgs)
	}
	return thread
}

func turnsFromMessages(threadID string, msgs []core.Message) []protocol.Turn {
	var turns []protocol.Turn
	var cur *protocol.Turn
	for _, msg := range msgs {
		if cur == nil || (msg.Role == core.RoleUser && !msg.Hidden) {
			if cur != nil {
				cur.Status = protocol.TurnStatusCompleted
				if cur.CompletedAt.IsZero() {
					cur.CompletedAt = cur.StartedAt
				}
				turns = append(turns, *cur)
			}
			cur = &protocol.Turn{
				ID:        "turn-" + msg.ID,
				ThreadID:  threadID,
				Status:    protocol.TurnStatusInProgress,
				StartedAt: msg.CreatedAt,
			}
		}
		if cur == nil {
			continue
		}
		cur.Items = append(cur.Items, itemsFromMessage(threadID, cur.ID, msg)...)
		if msg.UpdatedAt.After(cur.CompletedAt) {
			cur.CompletedAt = msg.UpdatedAt
		}
	}
	if cur != nil {
		cur.Status = protocol.TurnStatusCompleted
		if cur.CompletedAt.IsZero() {
			cur.CompletedAt = cur.StartedAt
		}
		turns = append(turns, *cur)
	}
	for i := range turns {
		if !turns[i].StartedAt.IsZero() && !turns[i].CompletedAt.IsZero() {
			turns[i].DurationMS = turns[i].CompletedAt.Sub(turns[i].StartedAt).Milliseconds()
		}
	}
	return turns
}

func itemsFromMessage(threadID, turnID string, msg core.Message) []protocol.ThreadItem {
	items := make([]protocol.ThreadItem, 0, 2+len(msg.ToolCalls)+len(msg.ToolResults))
	if strings.TrimSpace(msg.Text) != "" || msg.Role == core.RoleUser || msg.Role == core.RoleAssistant {
		itemType := protocol.ThreadItemNotice
		switch msg.Role {
		case core.RoleUser:
			itemType = protocol.ThreadItemUserMessage
		case core.RoleAssistant:
			itemType = protocol.ThreadItemAgentMessage
		}
		items = append(items, protocol.ThreadItem{
			ID:        msg.ID,
			ThreadID:  threadID,
			TurnID:    turnID,
			Type:      itemType,
			Role:      string(msg.Role),
			Text:      msg.Text,
			CreatedAt: msg.CreatedAt,
		})
	}
	if strings.TrimSpace(msg.Reasoning) != "" {
		items = append(items, protocol.ThreadItem{
			ID:        msg.ID + ":reasoning",
			ThreadID:  threadID,
			TurnID:    turnID,
			Type:      protocol.ThreadItemReasoning,
			Text:      msg.Reasoning,
			CreatedAt: msg.CreatedAt,
		})
	}
	for _, call := range msg.ToolCalls {
		items = append(items, protocol.ThreadItem{
			ID:         "tool_call:" + call.ID,
			ThreadID:   threadID,
			TurnID:     turnID,
			Type:       protocol.ThreadItemToolCall,
			ToolCallID: call.ID,
			ToolName:   call.Name,
			Input:      call.Input,
			CreatedAt:  msg.CreatedAt,
		})
	}
	for _, result := range msg.ToolResults {
		itemType := protocol.ThreadItemToolResult
		if result.IsError {
			itemType = protocol.ThreadItemError
		}
		items = append(items, protocol.ThreadItem{
			ID:         "tool_result:" + result.ToolCallID,
			ThreadID:   threadID,
			TurnID:     turnID,
			Type:       itemType,
			ToolCallID: result.ToolCallID,
			ToolName:   result.Name,
			Output:     result.Content,
			Metadata:   result.Metadata,
			CreatedAt:  msg.CreatedAt,
		})
	}
	return items
}

func EventServiceMessages(threadID string, ev Event, active protocol.Turn) []protocol.ServiceMessage {
	if ev.Kind == EventTurnDone {
		turnID := strings.TrimSpace(ev.TurnID)
		if turnID == "" {
			turnID = active.ID
		}
		if turnID == "" {
			return []protocol.ServiceMessage{
				{
					Type:         protocol.ServiceMessageTurnCompleted,
					ThreadID:     threadID,
					LastResponse: ev.LastResponse,
					Metadata:     ev.Metadata,
					CompletedAt:  time.Now(),
				},
				{
					Type:         protocol.ServiceMessageThreadStatusChanged,
					ThreadID:     threadID,
					ThreadStatus: protocol.ThreadStatusIdle,
				},
			}
		}
		completedAt := time.Now()
		startedAt := active.StartedAt
		turn := protocol.Turn{
			ID:          turnID,
			ThreadID:    threadID,
			Status:      protocol.TurnStatusCompleted,
			StartedAt:   startedAt,
			CompletedAt: completedAt,
		}
		if !startedAt.IsZero() {
			turn.DurationMS = completedAt.Sub(startedAt).Milliseconds()
		}
		return []protocol.ServiceMessage{
			{
				Type:         protocol.ServiceMessageTurnCompleted,
				ThreadID:     threadID,
				TurnID:       turnID,
				Turn:         &turn,
				LastResponse: ev.LastResponse,
				Metadata:     ev.Metadata,
				CompletedAt:  completedAt,
			},
			{
				Type:         protocol.ServiceMessageThreadStatusChanged,
				ThreadID:     threadID,
				ThreadStatus: protocol.ThreadStatusIdle,
			},
		}
	}
	if delta, ok := eventThreadItemDelta(ev); ok {
		return []protocol.ServiceMessage{{
			Type:     protocol.ServiceMessageItemDelta,
			ThreadID: threadID,
			TurnID:   ev.TurnID,
			Delta:    &delta,
		}}
	}
	if control, ok := eventControlMessage(ev); ok {
		return []protocol.ServiceMessage{{
			Type:     protocol.ServiceMessageControl,
			ThreadID: threadID,
			TurnID:   ev.TurnID,
			Control:  &control,
		}}
	}
	item, lifecycle := eventThreadItem(threadID, ev)
	if strings.TrimSpace(item.ID) == "" {
		return nil
	}
	msgType := protocol.ServiceMessageItemCompleted
	if lifecycle == "started" {
		msgType = protocol.ServiceMessageItemStarted
	}
	return []protocol.ServiceMessage{{
		Type:      msgType,
		ThreadID:  threadID,
		TurnID:    item.TurnID,
		Item:      &item,
		StartedAt: item.CreatedAt,
	}}
}

func ServiceMessageEvent(msg protocol.ServiceMessage) (Event, bool) {
	switch msg.Type {
	case protocol.ServiceMessageTurnCompleted:
		return Event{
			Kind:         EventTurnDone,
			TurnID:       msg.TurnID,
			LastResponse: msg.LastResponse,
			Metadata:     msg.Metadata,
		}, true
	case protocol.ServiceMessageItemDelta:
		if msg.Delta == nil {
			return Event{}, false
		}
		kind := EventInfo
		switch msg.Delta.Type {
		case protocol.ThreadItemDeltaAgentMessage:
			kind = EventAssistantDelta
		case protocol.ThreadItemDeltaReasoning:
			kind = EventReasoningDelta
		case protocol.ThreadItemDeltaPlan:
			kind = EventPlanDelta
		case protocol.ThreadItemDeltaTaskActivity:
			kind = EventTaskProgress
		case protocol.ThreadItemDeltaNotice:
			kind = EventBtwDelta
		default:
			return Event{}, false
		}
		return Event{Kind: kind, TurnID: msg.TurnID, ItemID: msg.Delta.ItemID, Text: msg.Delta.Text, Metadata: msg.Delta.Metadata}, true
	case protocol.ServiceMessageItemStarted, protocol.ServiceMessageItemCompleted:
		if msg.Item == nil {
			return Event{}, false
		}
		kind := eventKindFromThreadItem(msg)
		if kind == "" {
			return Event{}, false
		}
		item := msg.Item
		text := item.Text
		if text == "" {
			text = item.Output
		}
		if text == "" {
			text = item.Input
		}
		return Event{
			Kind:        kind,
			TurnID:      firstNonEmpty(msg.TurnID, item.TurnID),
			ItemID:      item.ID,
			ToolCallID:  item.ToolCallID,
			ToolName:    item.ToolName,
			Text:        text,
			Status:      item.Status,
			DurationMS:  item.DurationMS,
			Hook:        item.Hook,
			LocalResult: item.Local,
			Metadata:    item.Metadata,
		}, true
	case protocol.ServiceMessageControl:
		if msg.Control == nil {
			return Event{}, false
		}
		c := msg.Control
		kind := eventKindFromControl(c)
		if kind == "" {
			return Event{}, false
		}
		return Event{
			Kind:            kind,
			TurnID:          msg.TurnID,
			ClientInputID:   c.ClientInputID,
			ToolCallID:      firstNonEmpty(c.ToolCallID, toolCallIDFromApproval(c.Approval)),
			ToolName:        firstNonEmpty(c.ToolName, toolNameFromApproval(c.Approval)),
			ApprovalID:      c.ApprovalID,
			Decision:        c.Decision,
			DecisionScope:   c.DecisionScope,
			ApprovalKeys:    c.ApprovalKeys,
			Text:            c.Text,
			Status:          c.Status,
			Questions:       c.Questions,
			Choices:         c.Choices,
			Approval:        c.Approval,
			ModelChoices:    c.ModelChoices,
			EffortChoices:   c.EffortChoices,
			CurrentModel:    c.CurrentModel,
			CurrentEffort:   c.CurrentEffort,
			ThinkingChoices: c.ThinkingChoices,
			CurrentThinking: c.CurrentThinking,
			AutoAccept:      c.AutoAccept,
			AutoAcceptKnown: c.AutoAcceptKnown,
			ViewMode:        c.ViewMode,
			LocalResult:     c.Local,
			Skills:          c.Skills,
			Plugins:         c.Plugins,
			Config:          c.Config,
			Open:            c.Open,
			Hooks:           c.Hooks,
			WorktreeExit:    c.WorktreeExit,
			SessionID:       c.SessionID,
			Messages:        c.Messages,
			Metadata:        c.Metadata,
		}, true
	default:
		return Event{}, false
	}
}

func eventKindFromThreadItem(msg protocol.ServiceMessage) EventKind {
	if msg.Item == nil {
		return ""
	}
	switch msg.Item.Type {
	case protocol.ThreadItemAgentMessage:
		return EventAssistantDelta
	case protocol.ThreadItemReasoning:
		return EventReasoningDelta
	case protocol.ThreadItemPlan:
		if msg.Item.Status == "update" {
			return EventPlanUpdate
		}
		return EventPlanCompleted
	case protocol.ThreadItemToolCall:
		return EventToolCall
	case protocol.ThreadItemToolResult:
		return EventToolResult
	case protocol.ThreadItemHookRun:
		if msg.Type == protocol.ServiceMessageItemStarted {
			return EventHookStarted
		}
		return EventHookCompleted
	case protocol.ThreadItemTaskActivity:
		if msg.Type == protocol.ServiceMessageItemStarted {
			return EventTaskStarted
		}
		return EventTaskCompleted
	case protocol.ThreadItemWorkflowRun:
		if msg.Type == protocol.ServiceMessageItemStarted {
			return EventWorkflowSnapshot
		}
		return EventWorkflowTerminal
	case protocol.ThreadItemLocalCommand:
		return EventLocalSubmitResult
	case protocol.ThreadItemError:
		return EventError
	case protocol.ThreadItemContextCompaction:
		return EventResponseReset
	case protocol.ThreadItemNotice:
		return EventInfo
	default:
		return ""
	}
}

func eventKindFromControl(c *protocol.ControlMessage) EventKind {
	if c == nil {
		return ""
	}
	if c.EventKind != "" {
		return c.EventKind
	}
	switch c.Type {
	case protocol.ControlMessageApprovalRequired:
		return EventApprovalRequired
	case protocol.ControlMessageApprovalDecision:
		return EventApprovalDecision
	case protocol.ControlMessageUserInputRequired:
		return EventUserInputRequired
	case protocol.ControlMessageUserInputDone:
		return EventUserInputDone
	case protocol.ControlMessageSessionHydrated:
		return EventSessionHydrated
	case protocol.ControlMessageSessionsListed:
		return EventSessionsListed
	case protocol.ControlMessageRewindMessagesListed:
		return EventRewindMessagesListed
	case protocol.ControlMessageLocalSubmitResult:
		return EventLocalSubmitResult
	case protocol.ControlMessageLocalSubmitDone:
		return EventLocalSubmitDone
	case protocol.ControlMessageDiffResult:
		return EventDiffResult
	case protocol.ControlMessagePendingInputAccepted:
		return EventPendingInputAccepted
	case protocol.ControlMessagePendingInputRejected:
		return EventPendingInputRejected
	case protocol.ControlMessageViewModeChanged:
		return EventViewModeChanged
	case protocol.ControlMessageReviewRequested:
		return EventReviewRequested
	case protocol.ControlMessageScreenClearRequested:
		return EventScreenClearRequested
	case protocol.ControlMessageExitRequested:
		return EventExitRequested
	default:
		return ""
	}
}

func eventThreadItemDelta(ev Event) (protocol.ThreadItemDelta, bool) {
	itemID := stableEventItemID(ev)
	switch ev.Kind {
	case EventAssistantDelta:
		return protocol.ThreadItemDelta{ItemID: itemID, Type: protocol.ThreadItemDeltaAgentMessage, Text: ev.Text, Metadata: ev.Metadata}, true
	case EventReasoningDelta:
		return protocol.ThreadItemDelta{ItemID: itemID, Type: protocol.ThreadItemDeltaReasoning, Text: ev.Text, Metadata: ev.Metadata}, true
	case EventPlanDelta:
		return protocol.ThreadItemDelta{ItemID: itemID, Type: protocol.ThreadItemDeltaPlan, Text: ev.Text, Metadata: ev.Metadata}, true
	case EventTaskProgress:
		return protocol.ThreadItemDelta{ItemID: itemID, Type: protocol.ThreadItemDeltaTaskActivity, Text: ev.Text, Metadata: ev.Metadata}, true
	case EventBtwDelta:
		return protocol.ThreadItemDelta{ItemID: itemID, Type: protocol.ThreadItemDeltaNotice, Text: ev.Text, Metadata: ev.Metadata}, true
	default:
		return protocol.ThreadItemDelta{}, false
	}
}

func eventControlMessage(ev Event) (protocol.ControlMessage, bool) {
	controlType := protocol.ControlMessageUnknown
	switch ev.Kind {
	case EventInfo:
		if !ev.AutoAcceptKnown {
			return protocol.ControlMessage{}, false
		}
		controlType = protocol.ControlMessageUnknown
	case EventApprovalRequired:
		controlType = protocol.ControlMessageApprovalRequired
	case EventApprovalDecision:
		controlType = protocol.ControlMessageApprovalDecision
	case EventUserInputRequired:
		controlType = protocol.ControlMessageUserInputRequired
	case EventUserInputDone:
		controlType = protocol.ControlMessageUserInputDone
	case EventSessionHydrated:
		controlType = protocol.ControlMessageSessionHydrated
	case EventSessionsListed:
		controlType = protocol.ControlMessageSessionsListed
	case EventRewindMessagesListed:
		controlType = protocol.ControlMessageRewindMessagesListed
	case EventLocalSubmitResult:
		controlType = protocol.ControlMessageLocalSubmitResult
	case EventLocalSubmitDone:
		controlType = protocol.ControlMessageLocalSubmitDone
	case EventDiffResult:
		controlType = protocol.ControlMessageDiffResult
	case EventBtwStarted, EventBtwDone, EventBtwError, EventMCPStatus, EventMCPComplete, EventSkillLoaded, EventWorkflowPanel:
		controlType = protocol.ControlMessageUnknown
	case EventPendingInputAccepted:
		controlType = protocol.ControlMessagePendingInputAccepted
	case EventPendingInputRejected:
		controlType = protocol.ControlMessagePendingInputRejected
	case EventViewModeChanged:
		controlType = protocol.ControlMessageViewModeChanged
	case EventModelSelectionRequested, EventPermissionsSelectionRequested, EventSkillsSelectionRequested,
		EventSkillsManagerUpdated, EventPluginsManagerUpdated, EventConfigManagerUpdated,
		EventHooksManagerUpdated, EventHooksStartupReviewRequested:
		controlType = protocol.ControlMessageManagersUpdated
	case EventReviewRequested:
		controlType = protocol.ControlMessageReviewRequested
	case EventScreenClearRequested:
		controlType = protocol.ControlMessageScreenClearRequested
	case EventExitRequested, EventWorktreeExitPrompt:
		controlType = protocol.ControlMessageExitRequested
	default:
		return protocol.ControlMessage{}, false
	}
	return protocol.ControlMessage{
		Type:            controlType,
		Text:            ev.Text,
		Status:          ev.Status,
		EventKind:       ev.Kind,
		ClientInputID:   ev.ClientInputID,
		ToolCallID:      ev.ToolCallID,
		ToolName:        ev.ToolName,
		ApprovalID:      ev.ApprovalID,
		Decision:        ev.Decision,
		DecisionScope:   ev.DecisionScope,
		ApprovalKeys:    ev.ApprovalKeys,
		Choices:         ev.Choices,
		ModelChoices:    ev.ModelChoices,
		EffortChoices:   ev.EffortChoices,
		CurrentModel:    ev.CurrentModel,
		CurrentEffort:   ev.CurrentEffort,
		ThinkingChoices: ev.ThinkingChoices,
		CurrentThinking: ev.CurrentThinking,
		AutoAccept:      ev.AutoAccept,
		AutoAcceptKnown: ev.AutoAcceptKnown,
		ViewMode:        ev.ViewMode,
		Open:            ev.Open,
		Approval:        ev.Approval,
		Questions:       ev.Questions,
		Local:           ev.LocalResult,
		Config:          ev.Config,
		Hooks:           ev.Hooks,
		Skills:          ev.Skills,
		Plugins:         ev.Plugins,
		WorktreeExit:    ev.WorktreeExit,
		SessionID:       ev.SessionID,
		Messages:        ev.Messages,
		Metadata:        ev.Metadata,
	}, true
}

func eventThreadItem(threadID string, ev Event) (protocol.ThreadItem, string) {
	itemType := protocol.ThreadItemNotice
	lifecycle := "completed"
	switch ev.Kind {
	case EventError, EventBtwError:
		itemType = protocol.ThreadItemError
	case EventAssistantDelta:
		itemType = protocol.ThreadItemAgentMessage
	case EventReasoningDelta:
		itemType = protocol.ThreadItemReasoning
	case EventPlanCompleted, EventPlanUpdate:
		itemType = protocol.ThreadItemPlan
	case EventToolCall:
		itemType = protocol.ThreadItemToolCall
		lifecycle = "started"
	case EventToolResult:
		itemType = protocol.ThreadItemToolResult
	case EventHookStarted:
		itemType = protocol.ThreadItemHookRun
		lifecycle = "started"
	case EventHookCompleted:
		itemType = protocol.ThreadItemHookRun
	case EventTaskStarted:
		itemType = protocol.ThreadItemTaskActivity
		lifecycle = "started"
	case EventTaskCompleted:
		itemType = protocol.ThreadItemTaskActivity
	case EventWorkflowSnapshot:
		itemType = protocol.ThreadItemWorkflowRun
		lifecycle = "started"
	case EventWorkflowTerminal:
		itemType = protocol.ThreadItemWorkflowRun
	case EventLocalSubmitResult:
		itemType = protocol.ThreadItemLocalCommand
	case EventResponseReset:
		itemType = protocol.ThreadItemContextCompaction
	case EventInfo, EventProviderRetry, EventMCPStatus, EventMCPComplete, EventBtwStarted, EventBtwDone, EventSkillLoaded, EventWorkflowPanel:
		itemType = protocol.ThreadItemNotice
	default:
		return protocol.ThreadItem{}, ""
	}
	id := ev.ItemID
	if id == "" {
		id = stableEventItemID(ev)
	}
	status := ev.Status
	if ev.Kind == EventPlanUpdate && status == "" {
		status = "update"
	}
	return protocol.ThreadItem{
		ID:         id,
		ThreadID:   threadID,
		TurnID:     ev.TurnID,
		Type:       itemType,
		Text:       ev.Text,
		Summary:    status,
		ToolCallID: ev.ToolCallID,
		ToolName:   ev.ToolName,
		Input:      ev.Text,
		Output:     ev.Text,
		Status:     status,
		DurationMS: ev.DurationMS,
		Hook:       ev.Hook,
		Local:      ev.LocalResult,
		Metadata:   ev.Metadata,
		CreatedAt:  firstNonZeroTime(ev.StartedAt, time.Now()),
	}, lifecycle
}

func stableEventItemID(ev Event) string {
	if strings.TrimSpace(ev.ItemID) != "" {
		return ev.ItemID
	}
	if strings.TrimSpace(ev.ToolCallID) != "" {
		return fmt.Sprintf("%s:%s", ev.Kind, ev.ToolCallID)
	}
	if strings.TrimSpace(ev.WorkflowRunID) != "" {
		return fmt.Sprintf("%s:%s", ev.Kind, ev.WorkflowRunID)
	}
	if strings.TrimSpace(ev.TurnID) != "" {
		switch ev.Kind {
		case EventAssistantDelta:
			return "agent_message:" + ev.TurnID
		case EventReasoningDelta:
			return "reasoning:" + ev.TurnID
		case EventPlanDelta, EventPlanCompleted, EventPlanUpdate:
			return "plan:" + ev.TurnID
		}
	}
	return fmt.Sprintf("event:%d:%s", ev.Sequence, ev.Kind)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func toolCallIDFromApproval(approval *protocol.ApprovalRequest) string {
	if approval == nil {
		return ""
	}
	return approval.ToolCall.ID
}

func toolNameFromApproval(approval *protocol.ApprovalRequest) string {
	if approval == nil {
		return ""
	}
	return approval.ToolCall.Name
}

func firstNonZeroTime(values ...time.Time) time.Time {
	for _, value := range values {
		if !value.IsZero() {
			return value
		}
	}
	return time.Time{}
}
