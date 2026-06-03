package protocol

import "time"

type ServiceMessageType string

const (
	ServiceMessageTurnStarted         ServiceMessageType = "turn_started"
	ServiceMessageTurnCompleted       ServiceMessageType = "turn_completed"
	ServiceMessageThreadStatusChanged ServiceMessageType = "thread_status_changed"
	ServiceMessageItemStarted         ServiceMessageType = "item_started"
	ServiceMessageItemDelta           ServiceMessageType = "item_delta"
	ServiceMessageItemCompleted       ServiceMessageType = "item_completed"
	ServiceMessageControl             ServiceMessageType = "control"
)

type ServiceMessage struct {
	Type         ServiceMessageType `json:"type"`
	ThreadID     string             `json:"thread_id,omitempty"`
	TurnID       string             `json:"turn_id,omitempty"`
	Item         *ThreadItem        `json:"item,omitempty"`
	Delta        *ThreadItemDelta   `json:"delta,omitempty"`
	Turn         *Turn              `json:"turn,omitempty"`
	ThreadStatus ThreadStatus       `json:"thread_status,omitempty"`
	Control      *ControlMessage    `json:"control,omitempty"`
	LastResponse string             `json:"last_response,omitempty"`
	Metadata     map[string]any     `json:"metadata,omitempty"`
	StartedAt    time.Time          `json:"started_at,omitempty"`
	CompletedAt  time.Time          `json:"completed_at,omitempty"`
}

type ControlMessageType string

const (
	ControlMessageApprovalRequired     ControlMessageType = "approval_required"
	ControlMessageApprovalDecision     ControlMessageType = "approval_decision"
	ControlMessageUserInputRequired    ControlMessageType = "user_input_required"
	ControlMessageUserInputDone        ControlMessageType = "user_input_done"
	ControlMessageSessionHydrated      ControlMessageType = "session_hydrated"
	ControlMessageSessionsListed       ControlMessageType = "sessions_listed"
	ControlMessageRewindMessagesListed ControlMessageType = "rewind_messages_listed"
	ControlMessageLocalSubmitResult    ControlMessageType = "local_submit_result"
	ControlMessageLocalSubmitDone      ControlMessageType = "local_submit_done"
	ControlMessageDiffResult           ControlMessageType = "diff_result"
	ControlMessagePendingInputAccepted ControlMessageType = "pending_input_accepted"
	ControlMessagePendingInputRejected ControlMessageType = "pending_input_rejected"
	ControlMessageViewModeChanged      ControlMessageType = "view_mode_changed"
	ControlMessageManagersUpdated      ControlMessageType = "managers_updated"
	ControlMessageReviewRequested      ControlMessageType = "review_requested"
	ControlMessageScreenClearRequested ControlMessageType = "screen_clear_requested"
	ControlMessageExitRequested        ControlMessageType = "exit_requested"
	ControlMessageUnknown              ControlMessageType = "unknown"
)

type ControlMessage struct {
	Type            ControlMessageType   `json:"type"`
	Text            string               `json:"text,omitempty"`
	Status          string               `json:"status,omitempty"`
	EventKind       EventKind            `json:"event_kind,omitempty"`
	ClientInputID   string               `json:"client_input_id,omitempty"`
	ApprovalID      string               `json:"approval_id,omitempty"`
	Decision        string               `json:"decision,omitempty"`
	DecisionScope   string               `json:"decision_scope,omitempty"`
	ApprovalKeys    []string             `json:"approval_keys,omitempty"`
	Choices         []string             `json:"choices,omitempty"`
	ModelChoices    []string             `json:"model_choices,omitempty"`
	EffortChoices   []string             `json:"effort_choices,omitempty"`
	CurrentModel    string               `json:"current_model,omitempty"`
	CurrentEffort   string               `json:"current_effort,omitempty"`
	ThinkingChoices []string             `json:"thinking_choices,omitempty"`
	CurrentThinking string               `json:"current_thinking,omitempty"`
	AutoAccept      bool                 `json:"auto_accept,omitempty"`
	AutoAcceptKnown bool                 `json:"auto_accept_known,omitempty"`
	ViewMode        string               `json:"view_mode,omitempty"`
	Open            bool                 `json:"open,omitempty"`
	Approval        *ApprovalRequest     `json:"approval,omitempty"`
	Questions       []UserInputQuestion  `json:"questions,omitempty"`
	Local           *LocalResult         `json:"local,omitempty"`
	Config          *ConfigManagerState  `json:"config,omitempty"`
	Hooks           *HooksManagerState   `json:"hooks,omitempty"`
	Skills          []SkillView          `json:"skills,omitempty"`
	Plugins         []PluginStatus       `json:"plugins,omitempty"`
	WorktreeExit    *WorktreeExitSummary `json:"worktree_exit,omitempty"`
	SessionID       string               `json:"session_id,omitempty"`
	Messages        []Message            `json:"messages,omitempty"`
	Metadata        map[string]any       `json:"metadata,omitempty"`
}
