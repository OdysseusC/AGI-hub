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
	CommunicationLanguage        string   `json:"communication_language,omitempty"`
	PeerProtocolVersion          int      `json:"peer_protocol_version,omitempty"`
	PeerWhileBusyVerified        bool     `json:"peer_while_busy_verified,omitempty"`
	MailboxProtocolVersion       int      `json:"mailbox_protocol_version,omitempty"`
	MailboxWhileBusyVerified     bool     `json:"mailbox_while_busy_verified,omitempty"`
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
	ParentTaskID      string     `json:"parent_task_id,omitempty"`
	OriginRef         string     `json:"origin_ref,omitempty"`
	MaxHops           int        `json:"max_hops,omitempty"`
}

// OriginContext pins the native session where the human started a collaboration.
// A delegated message cannot replace it with the sender's current session.
type OriginContext struct {
	Endpoint   string `json:"endpoint"`
	DeviceID   string `json:"device_id"`
	SessionID  string `json:"session_id"`
	RequestRef string `json:"request_ref,omitempty"`
}

// Collaboration retains diagnostic records from the unpublished v2 prototype.
// New mailbox messages never create or interpret this former workflow state.
type Collaboration struct {
	ID           string        `json:"id"`
	RootTaskID   string        `json:"root_task_id"`
	RootOrigin   OriginContext `json:"root_origin"`
	MaxHandoffs  int           `json:"max_handoffs"`
	UsedHandoffs int           `json:"used_handoffs"`
	MaxHops      int           `json:"max_hops"`
	Deadline     string        `json:"deadline"`
	Closed       bool          `json:"closed,omitempty"`
}

type Envelope struct {
	CommunicationLanguage string `json:"communication_language,omitempty"`
	// SourceMessageID maps a canonical LAN ID to the caller's saved retry ID.
	// It is populated only in client-facing network responses.
	SourceMessageID string         `json:"source_message_id,omitempty"`
	Network         *NetworkMail   `json:"network,omitempty"`
	Mailbox         bool           `json:"mailbox,omitempty"`
	MailFingerprint string         `json:"mail_fingerprint,omitempty"`
	ID              string         `json:"message_id"`
	TaskID          string         `json:"task_id"`
	ReplyTo         string         `json:"reply_to,omitempty"` // message being answered
	CorrelationID   string         `json:"correlation_id,omitempty"`
	ParentTaskID    string         `json:"parent_task_id,omitempty"`
	ParentRunID     string         `json:"parent_run_id,omitempty"`
	ResumeConsumed  bool           `json:"resume_consumed,omitempty"`
	NativeSettled   bool           `json:"native_settled,omitempty"`
	RootOrigin      *OriginContext `json:"root_origin,omitempty"`
	Kind            string         `json:"kind"` // mailbox: request, reply, status, result; legacy: task, control, result
	Sender          string         `json:"sender"`
	Recipient       string         `json:"recipient"`
	Mode            string         `json:"mode"`
	TargetRunID     string         `json:"target_run_id,omitempty"`
	Content         string         `json:"content"`
	Delivery        string         `json:"delivery"` // pending, dispatching, delivered, uncertain, failed
	Detail          string         `json:"detail,omitempty"`
}

// MailSubmission is addressed data. ReplyTo and RootOrigin provide context;
// neither tells the broker where a reply must go or what an assistant must do.
type MailSubmission struct {
	ID            string `json:"id"`
	Recipient     string `json:"recipient"`
	Kind          string `json:"kind"`
	Content       string `json:"content"`
	CorrelationID string `json:"correlation_id,omitempty"`
	OriginRef     string `json:"origin_ref,omitempty"`
	ReplyTo       string `json:"reply_to,omitempty"`
	Deadline      string `json:"deadline,omitempty"`
	MaxMessages   int    `json:"max_messages,omitempty"`
}

type MailConversation struct {
	Federation   *FederatedContext `json:"federation,omitempty"`
	ID           string            `json:"id"`
	RootOrigin   OriginContext     `json:"root_origin"`
	Participants map[string]bool   `json:"participants"`
	MaxMessages  int               `json:"max_messages"`
	UsedMessages int               `json:"used_messages"`
	Deadline     string            `json:"deadline"`
	Closed       bool              `json:"closed,omitempty"`
}

type Task struct {
	Submission
	CorrelationID string         `json:"correlation_id,omitempty"`
	RootOrigin    *OriginContext `json:"root_origin,omitempty"`
	Hop           int            `json:"hop,omitempty"`
	Coordinator   string         `json:"coordinator"`
	LeaderEpoch   int            `json:"leader_epoch"`
	Execution     string         `json:"execution"` // waiting, accepted, running, waiting_reply, needs_user, succeeded, failed, interrupted
	RunID         string         `json:"run_id,omitempty"`
	Summary       string         `json:"summary,omitempty"`
	Artifacts     []Artifact     `json:"artifacts,omitempty"`
}

type Report struct {
	EventID         string     `json:"event_id"`
	TaskID          string     `json:"task_id"`
	Endpoint        string     `json:"endpoint"`
	State           string     `json:"state"`
	RunID           string     `json:"run_id,omitempty"`
	Summary         string     `json:"summary,omitempty"`
	Artifacts       []Artifact `json:"artifacts,omitempty"`
	ResumeMessageID string     `json:"resume_message_id,omitempty"`
}

type Snapshot struct {
	Version           int                         `json:"version"`
	Leader            string                      `json:"leader"`
	LeaderEpoch       int                         `json:"leader_epoch"`
	Endpoints         map[string]Endpoint         `json:"endpoints"`
	Sessions          map[string]Session          `json:"sessions"`
	Tasks             map[string]Task             `json:"tasks"`
	Messages          map[string]Envelope         `json:"messages"`
	EventIDs          map[string]string           `json:"event_ids"`
	ResourceOwners    map[string]string           `json:"resource_owners"`
	Collaborations    map[string]Collaboration    `json:"collaborations,omitempty"`
	MailConversations map[string]MailConversation `json:"mail_conversations,omitempty"`
}
