package domain

import "time"

// SessionID identifies one session.
type SessionID string

// sessionIDTokenLen is the number of random characters in a session ID.
const sessionIDTokenLen = 6

// NewSessionID returns an ID such as "20261005T140212Z-K3F9A2". The time
// makes IDs sort by start, and token keeps two IDs of the same second apart.
func NewSessionID(start time.Time, token string) SessionID {
	if len(token) > sessionIDTokenLen {
		token = token[:sessionIDTokenLen]
	}
	return SessionID(start.UTC().Format("20060102T150405Z") + "-" + token)
}

// SessionStatus is the state of a session.
type SessionStatus string

const (
	StatusRunning   SessionStatus = "running"
	StatusCompleted SessionStatus = "completed"
	StatusAborted   SessionStatus = "aborted"
	StatusFailed    SessionStatus = "failed"
)

// Session holds the facts that are the same for every exchange of a session.
type Session struct {
	ID            SessionID
	StartedAt     time.Time
	ToolVersion   string
	OllamaVersion string
	Host          Host
	Tag           string
	Workload      Workload
	Order         Order
	Interval      time.Duration
	Runs          int
	CacheModes    []CacheMode
}

// Power sources of a Host.
const (
	PowerAC      = "ac"
	PowerBattery = "battery"
	PowerUnknown = "unknown"
)

// Host describes the machine that runs the session.
type Host struct {
	Name        string
	PowerSource string // PowerAC, PowerBattery, or PowerUnknown
}
