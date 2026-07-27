package model

import "time"

type State string

const (
	StateIdle       State = "idle"
	StateRunning    State = "running"
	StateNeedsInput State = "needs_input"
	StateReady      State = "ready"
	StateBlocked    State = "blocked"
)

type Reason string

const (
	ReasonWorking    Reason = "working"
	ReasonQuestion   Reason = "question"
	ReasonApproval   Reason = "approval"
	ReasonPermission Reason = "permission"
	ReasonCompleted  Reason = "completed"
	ReasonFailed     Reason = "failed"
)

type Task struct {
	ID            string    `json:"id"`
	Title         string    `json:"title"`
	State         State     `json:"state"`
	Reason        Reason    `json:"reason"`
	UpdatedAt     time.Time `json:"updatedAt"`
	SubagentCount int       `json:"subagentCount"`
	Acknowledged  bool      `json:"-"`
}

type Counts struct {
	NeedsInput int `json:"needsInput"`
	Blocked    int `json:"blocked"`
	Ready      int `json:"ready"`
	Running    int `json:"running"`
}

type DeviceMetrics struct {
	TemperatureC    *float64 `json:"temperatureC,omitempty"`
	HumidityRH      *float64 `json:"humidityRh,omitempty"`
	BatteryVoltage  *float64 `json:"batteryVoltage,omitempty"`
	BatteryPercent  *int     `json:"batteryPercent,omitempty"`
	BatteryEstimate bool     `json:"batteryEstimate"`
	Presence        bool     `json:"presence"`
	WiFiRSSI        int      `json:"wifiRssi,omitempty"`
	SensorUpdatedAt int64    `json:"sensorUpdatedAt,omitempty"`
}

type Snapshot struct {
	Type           string         `json:"type"`
	Version        int            `json:"version"`
	Seq            uint64         `json:"seq"`
	GeneratedAt    time.Time      `json:"generatedAt"`
	AggregateState State          `json:"aggregateState"`
	Tasks          []Task         `json:"tasks"`
	Counts         Counts         `json:"counts"`
	Device         *DeviceMetrics `json:"device,omitempty"`
}

type FocusAction struct {
	Type    string `json:"type"`
	Version int    `json:"version"`
	TaskID  string `json:"taskId"`
	SeenSeq uint64 `json:"seenSeq"`
}

// HookEvent is deliberately content-free. It must never contain prompts,
// transcript paths, tool inputs, tool responses, commands, or full cwd paths.
type HookEvent struct {
	EventID   string    `json:"eventId"`
	SessionID string    `json:"sessionId"`
	TurnID    string    `json:"turnId,omitempty"`
	Event     string    `json:"event"`
	ToolName  string    `json:"toolName,omitempty"`
	ToolUseID string    `json:"toolUseId,omitempty"`
	AgentID   string    `json:"agentId,omitempty"`
	Workspace string    `json:"workspace,omitempty"`
	At        time.Time `json:"at"`
}

func Rank(state State) int {
	switch state {
	case StateNeedsInput:
		return 4
	case StateBlocked:
		return 3
	case StateReady:
		return 2
	case StateRunning:
		return 1
	default:
		return 0
	}
}
