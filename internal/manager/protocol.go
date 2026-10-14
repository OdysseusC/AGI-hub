package manager

// Endpoint is a user-configured binding to one native assistant session.
type Endpoint struct {
	ID                           string   `json:"id"`
	DeviceID                     string   `json:"device_id"`
	SessionID                    string   `json:"session_id"`
	Enabled                      bool     `json:"enabled"`
	IdleWakeVerified             bool     `json:"idle_wake_verified"`
	ResultQueueWhileBusyVerified bool     `json:"result_queue_while_busy_verified,omitempty"`
	Modes                        []string `json:"modes"`
	Driver                       string   `json:"driver"`
}

type Session struct {
	State string `json:"state"` // unknown, idle, busy, needs_user
	RunID string `json:"run_id,omitempty"`
}

type Artifact struct {
	DeviceID    string `json:"device_id"`
	WorkspaceID string `json:"workspace_id"`
	Path        string `json:"path"` // relative to a configured workspace
	SHA256      string `json:"sha256,omitempty"`
}

type Submission struct {
	ID                string     `json:"id"` // stable caller-generated idempotency key
	Origin            string     `json:"origin"`
	Recipient         string     `json:"recipient"`
	Goal              string     `json:"goal"`
	Context           string     `json:"context,omitempty"`
	Inputs            []Artifact `json:"inputs,omitempty"`
	Mode              string     `json:"mode"` // queue, steer, interrupt
	TargetRunID       string     `json:"target_run_id,omitempty"`
	Resources         []string   `json:"resources,omitempty"`
	RequireArtifact   bool       `json:"require_artifact"`
	AuthorizedActions []string   `json:"authorized_actions,omitempty"`
	MaxHandoffs       int        `json:"max_handoffs"`
	Deadline          string     `json:"deadline,omitempty"`
}

type Envelope struct {
	ID            string `json:"message_id"`
	TaskID        string `json:"task_id"`
	ReplyTo       string `json:"reply_to,omitempty"`       // message being answered
	CorrelationID string `json:"correlation_id,omitempty"` // V0 keeps the originating task ID
	Kind          string `json:"kind"`                     // task, control, result
	Sender        string `json:"sender"`
	Recipient     string `json:"recipient"`
	Mode          string `json:"mode"`
	TargetRunID   string `json:"target_run_id,omitempty"`
	Content       string `json:"content"`
	Delivery      string `json:"delivery"` // pending, dispatching, delivered, uncertain, failed
	Detail        string `json:"detail,omitempty"`
}

type Task struct {
	Submission
	Coordinator string     `json:"coordinator"`
	LeaderEpoch int        `json:"leader_epoch"`
	Execution   string     `json:"execution"` // waiting, accepted, running, needs_user, succeeded, failed, interrupted
	RunID       string     `json:"run_id,omitempty"`
	Summary     string     `json:"summary,omitempty"`
	Artifacts   []Artifact `json:"artifacts,omitempty"`
}

type Report struct {
	EventID   string     `json:"event_id"`
	TaskID    string     `json:"task_id"`
	Endpoint  string     `json:"endpoint"`
	State     string     `json:"state"`
	RunID     string     `json:"run_id,omitempty"`
	Summary   string     `json:"summary,omitempty"`
	Artifacts []Artifact `json:"artifacts,omitempty"`
}

type Snapshot struct {
	Version        int                 `json:"version"`
	Leader         string              `json:"leader"`
	LeaderEpoch    int                 `json:"leader_epoch"`
	Endpoints      map[string]Endpoint `json:"endpoints"`
	Sessions       map[string]Session  `json:"sessions"`
	Tasks          map[string]Task     `json:"tasks"`
	Messages       map[string]Envelope `json:"messages"`
	EventIDs       map[string]string   `json:"event_ids"`
	ResourceOwners map[string]string   `json:"resource_owners"`
}
