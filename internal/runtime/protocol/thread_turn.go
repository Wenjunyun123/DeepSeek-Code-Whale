package protocol

import "time"

type ThreadStatus string

const (
	ThreadStatusNotLoaded   ThreadStatus = "not_loaded"
	ThreadStatusIdle        ThreadStatus = "idle"
	ThreadStatusActive      ThreadStatus = "active"
	ThreadStatusSystemError ThreadStatus = "system_error"
)

type TurnStatus string

const (
	TurnStatusInProgress  TurnStatus = "in_progress"
	TurnStatusCompleted   TurnStatus = "completed"
	TurnStatusInterrupted TurnStatus = "interrupted"
	TurnStatusFailed      TurnStatus = "failed"
)

type Thread struct {
	ID        string       `json:"id"`
	SessionID string       `json:"session_id"`
	Status    ThreadStatus `json:"status"`
	CWD       string       `json:"cwd,omitempty"`
	Model     string       `json:"model,omitempty"`
	Source    string       `json:"source,omitempty"`
	Title     string       `json:"title,omitempty"`
	Summary   string       `json:"summary,omitempty"`
	CreatedAt time.Time    `json:"created_at,omitempty"`
	UpdatedAt time.Time    `json:"updated_at,omitempty"`
	Turns     []Turn       `json:"turns,omitempty"`
}

type Turn struct {
	ID          string       `json:"id"`
	ThreadID    string       `json:"thread_id"`
	Status      TurnStatus   `json:"status"`
	Items       []ThreadItem `json:"items,omitempty"`
	StartedAt   time.Time    `json:"started_at,omitempty"`
	CompletedAt time.Time    `json:"completed_at,omitempty"`
	DurationMS  int64        `json:"duration_ms,omitempty"`
	Error       *TurnError   `json:"error,omitempty"`
}

type TurnError struct {
	Message string `json:"message"`
}

type ThreadItemType string

const (
	ThreadItemUserMessage       ThreadItemType = "user_message"
	ThreadItemAgentMessage      ThreadItemType = "agent_message"
	ThreadItemReasoning         ThreadItemType = "reasoning"
	ThreadItemPlan              ThreadItemType = "plan"
	ThreadItemToolCall          ThreadItemType = "tool_call"
	ThreadItemToolResult        ThreadItemType = "tool_result"
	ThreadItemHookRun           ThreadItemType = "hook_run"
	ThreadItemTaskActivity      ThreadItemType = "task_activity"
	ThreadItemWorkflowRun       ThreadItemType = "workflow_run"
	ThreadItemLocalCommand      ThreadItemType = "local_command"
	ThreadItemNotice            ThreadItemType = "notice"
	ThreadItemError             ThreadItemType = "error"
	ThreadItemContextCompaction ThreadItemType = "context_compaction"
)

type ThreadItem struct {
	ID         string         `json:"id"`
	ThreadID   string         `json:"thread_id"`
	TurnID     string         `json:"turn_id,omitempty"`
	Type       ThreadItemType `json:"type"`
	Role       string         `json:"role,omitempty"`
	Text       string         `json:"text,omitempty"`
	Summary    string         `json:"summary,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
	ToolName   string         `json:"tool_name,omitempty"`
	Input      string         `json:"input,omitempty"`
	Output     string         `json:"output,omitempty"`
	Status     string         `json:"status,omitempty"`
	DurationMS int64          `json:"duration_ms,omitempty"`
	Hook       *HookRun       `json:"hook,omitempty"`
	Local      *LocalResult   `json:"local,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
	CreatedAt  time.Time      `json:"created_at,omitempty"`
	UpdatedAt  time.Time      `json:"updated_at,omitempty"`
}

type ThreadItemDeltaKind string

const (
	ThreadItemDeltaAgentMessage ThreadItemDeltaKind = "agent_message"
	ThreadItemDeltaReasoning    ThreadItemDeltaKind = "reasoning"
	ThreadItemDeltaPlan         ThreadItemDeltaKind = "plan"
	ThreadItemDeltaTaskActivity ThreadItemDeltaKind = "task_activity"
	ThreadItemDeltaCommandOut   ThreadItemDeltaKind = "command_output"
	ThreadItemDeltaNotice       ThreadItemDeltaKind = "notice"
)

type ThreadItemDelta struct {
	ItemID   string              `json:"item_id"`
	Type     ThreadItemDeltaKind `json:"type"`
	Text     string              `json:"text,omitempty"`
	Metadata map[string]any      `json:"metadata,omitempty"`
}

type ThreadCurrentResponse struct {
	Thread Thread `json:"thread"`
}

type ThreadListParams struct {
	Limit int `json:"limit,omitempty"`
}

type ThreadListResponse struct {
	Threads []Thread `json:"threads"`
}

type ThreadReadParams struct {
	ThreadID     string `json:"thread_id"`
	IncludeTurns bool   `json:"include_turns,omitempty"`
}

type ThreadReadResponse struct {
	Thread Thread `json:"thread"`
}

type ThreadStartParams struct{}

type ThreadStartResponse struct {
	Thread Thread `json:"thread"`
}

type ThreadResumeParams struct {
	ThreadID string `json:"thread_id"`
}

type ThreadResumeResponse struct {
	Thread Thread `json:"thread"`
}

type TurnStartParams struct {
	ThreadID string      `json:"thread_id,omitempty"`
	Input    []UserInput `json:"input"`
}

type UserInputKind string

const (
	UserInputText       UserInputKind = "text"
	UserInputImage      UserInputKind = "image"
	UserInputLocalImage UserInputKind = "local_image"
	UserInputMention    UserInputKind = "mention"
	UserInputSkill      UserInputKind = "skill"
)

type UserInput struct {
	Kind UserInputKind `json:"kind"`
	Text string        `json:"text,omitempty"`
}

type TurnStartResponse struct {
	Turn Turn `json:"turn"`
}

type TurnInterruptParams struct {
	ThreadID string `json:"thread_id"`
	TurnID   string `json:"turn_id"`
}

type TurnInterruptResponse struct {
	Interrupted bool `json:"interrupted"`
}

type ThreadStartedNotification struct {
	Thread Thread `json:"thread"`
}

type ThreadStatusChangedNotification struct {
	ThreadID string       `json:"thread_id"`
	Status   ThreadStatus `json:"status"`
}

type TurnStartedNotification struct {
	ThreadID string `json:"thread_id"`
	Turn     Turn   `json:"turn"`
}

type TurnCompletedNotification struct {
	ThreadID string `json:"thread_id"`
	Turn     Turn   `json:"turn"`
}

type ItemStartedNotification struct {
	ThreadID    string     `json:"thread_id"`
	TurnID      string     `json:"turn_id,omitempty"`
	Item        ThreadItem `json:"item"`
	StartedAtMS int64      `json:"started_at_ms,omitempty"`
}

type ItemDeltaNotification struct {
	ThreadID string          `json:"thread_id"`
	TurnID   string          `json:"turn_id,omitempty"`
	ItemID   string          `json:"item_id"`
	Delta    ThreadItemDelta `json:"delta"`
}

type ItemCompletedNotification struct {
	ThreadID      string     `json:"thread_id"`
	TurnID        string     `json:"turn_id,omitempty"`
	Item          ThreadItem `json:"item"`
	CompletedAtMS int64      `json:"completed_at_ms,omitempty"`
}
