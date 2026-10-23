package manager

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Broker owns a durable journal. Every mutation becomes visible only after fsync.
type Broker struct {
	mu          sync.Mutex
	state       Snapshot
	journal     *os.File
	processLock *os.File
	deviceID    string
	workspaces  map[string]string
	order       []string
	subscribers map[chan struct{}]struct{}
	closed      bool
	writeErr    error
}

func emptySnapshot() Snapshot {
	return Snapshot{Version: 1, Endpoints: map[string]Endpoint{}, Sessions: map[string]Session{}, Tasks: map[string]Task{}, Messages: map[string]Envelope{}, EventIDs: map[string]string{}, ResourceOwners: map[string]string{}}
}

func Open(dir string, deviceID string, workspaces map[string]string) (*Broker, error) {
	if strings.TrimSpace(dir) == "" || strings.TrimSpace(deviceID) == "" {
		return nil, errors.New("data directory and device ID are required")
	}
	if err := checkIdentifier("device ID", deviceID); err != nil {
		return nil, err
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(abs, 0700); err != nil {
		return nil, err
	}
	abs, err = filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, err
	}
	lock, err := acquireDirectoryLock(filepath.Clean(abs))
	if err != nil {
		return nil, err
	}
	roots := make(map[string]string, len(workspaces))
	for id, root := range workspaces {
		if err := checkIdentifier("workspace ID", id); err != nil {
			lock.Close()
			return nil, err
		}
		if strings.TrimSpace(id) == "" || strings.TrimSpace(root) == "" {
			lock.Close()
			return nil, errors.New("workspace ID and path are required")
		}
		root, err = filepath.Abs(root)
		if err == nil {
			root, err = filepath.EvalSymlinks(root)
		}
		if err != nil {
			lock.Close()
			return nil, fmt.Errorf("workspace %s: %w", id, err)
		}
		info, statErr := os.Stat(root)
		if statErr != nil || !info.IsDir() {
			lock.Close()
			return nil, fmt.Errorf("workspace %s is not an existing directory", id)
		}
		roots[id] = filepath.Clean(root)
	}
	file, state, order, err := openJournal(abs)
	if err != nil {
		lock.Close()
		return nil, err
	}
	b := &Broker{state: state, journal: file, processLock: lock, deviceID: deviceID, workspaces: roots, order: order, subscribers: make(map[chan struct{}]struct{})}
	recovered := cloneSnapshot(state)
	changed := false
	for id, envelope := range recovered.Messages {
		if envelope.Delivery == "dispatching" {
			envelope.Delivery = "uncertain"
			envelope.Detail = "process restarted before delivery was confirmed"
			recovered.Messages[id] = envelope
			changed = true
		}
	}
	for id, session := range recovered.Sessions {
		if session.State != "unknown" {
			session.State = "unknown"
			recovered.Sessions[id] = session
			changed = true
		}
	}
	for _, task := range recovered.Tasks {
		if !terminal(task.Execution) {
			session := recovered.Sessions[task.Recipient]
			if session.State != "unknown" {
				session.State = "unknown"
				recovered.Sessions[task.Recipient] = session
				changed = true
			}
		}
	}
	info, statErr := file.Stat()
	if statErr != nil {
		b.Close()
		return nil, statErr
	}
	if changed || info.Size() == 0 {
		if err = b.commit(recovered); err != nil {
			b.Close()
			return nil, err
		}
	}
	return b, nil
}

func (b *Broker) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil
	}
	b.closed = true
	for channel := range b.subscribers {
		close(channel)
		delete(b.subscribers, channel)
	}
	return errors.Join(b.journal.Close(), b.processLock.Close())
}

func (b *Broker) Snapshot() Snapshot { b.mu.Lock(); defer b.mu.Unlock(); return cloneSnapshot(b.state) }

// Signal notifies state subscribers about a directory/transport change without
// fabricating a mailbox event or writing an unchanged journal snapshot.
func (b *Broker) Signal() {
	b.mu.Lock()
	defer b.mu.Unlock()
	for channel := range b.subscribers {
		select {
		case channel <- struct{}{}:
		default:
		}
	}
}

func (b *Broker) writable() error {
	if b.closed {
		return errors.New("broker is closed")
	}
	if b.writeErr != nil {
		return fmt.Errorf("journal write previously failed; restart and inspect recovery: %w", b.writeErr)
	}
	return nil
}

// commit is called while holding mu (or before Open publishes the broker).
func (b *Broker) commit(next Snapshot) error {
	if err := b.writable(); err != nil {
		return err
	}
	if err := appendSnapshot(b.journal, next); err != nil {
		b.writeErr = err
		return fmt.Errorf("persist state: %w", err)
	}
	for _, id := range sortedMessageIDs(next.Messages) {
		if _, exists := b.state.Messages[id]; !exists {
			b.order = append(b.order, id)
		}
	}
	b.state = next
	for channel := range b.subscribers {
		select {
		case channel <- struct{}{}:
		default:
		}
	}
	return nil
}

func (b *Broker) Subscribe() (<-chan struct{}, func()) {
	b.mu.Lock()
	defer b.mu.Unlock()
	channel := make(chan struct{}, 1)
	if b.closed {
		close(channel)
	} else {
		b.subscribers[channel] = struct{}{}
	}
	var once sync.Once
	return channel, func() {
		once.Do(func() {
			b.mu.Lock()
			defer b.mu.Unlock()
			if _, ok := b.subscribers[channel]; ok {
				delete(b.subscribers, channel)
				close(channel)
			}
		})
	}
}

func (b *Broker) Register(endpoint Endpoint) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.writable(); err != nil {
		return err
	}
	if err := normalizeEndpointLanguage(&endpoint); err != nil {
		return err
	}
	if strings.TrimSpace(endpoint.ID) == "" || strings.TrimSpace(endpoint.DeviceID) == "" || strings.TrimSpace(endpoint.Driver) == "" {
		return errors.New("endpoint ID, device, and driver are required")
	}
	if endpoint.MailboxProtocolVersion < 0 || endpoint.MailboxProtocolVersion > 1 || endpoint.MailboxWhileBusyVerified && endpoint.MailboxProtocolVersion != 1 {
		return errors.New("mailbox protocol must be 0 or 1; mailbox busy guidance requires protocol 1")
	}
	for label, value := range map[string]string{"endpoint ID": endpoint.ID, "device ID": endpoint.DeviceID, "driver": endpoint.Driver} {
		if err := checkIdentifier(label, value); err != nil {
			return err
		}
	}
	if endpoint.Enabled || endpoint.SessionID != "" {
		if err := checkIdentifier("session ID", endpoint.SessionID); err != nil {
			return err
		}
	}
	for id, existing := range b.state.Endpoints {
		if endpoint.SessionID != "" && id != endpoint.ID && existing.DeviceID == endpoint.DeviceID && existing.SessionID == endpoint.SessionID {
			return errors.New("native session is already registered under another endpoint")
		}
	}
	seen := map[string]bool{}
	for _, mode := range endpoint.Modes {
		if !validMode(mode) || seen[mode] {
			return errors.New("endpoint modes must be unique queue, steer, or interrupt values")
		}
		seen[mode] = true
	}
	if old, exists := b.state.Endpoints[endpoint.ID]; exists && (old.DeviceID != endpoint.DeviceID || old.SessionID != endpoint.SessionID || old.Driver != endpoint.Driver) {
		for _, task := range b.state.Tasks {
			if (task.Recipient == endpoint.ID || task.Origin == endpoint.ID) && !terminal(task.Execution) {
				return errors.New("cannot rebind an endpoint with unfinished tasks")
			}
		}
		for _, envelope := range b.state.Messages {
			if envelope.Recipient == endpoint.ID && (envelope.Delivery == "pending" || envelope.Delivery == "dispatching" || envelope.Delivery == "uncertain") {
				return errors.New("cannot rebind an endpoint with unresolved messages")
			}
		}
	}
	next := cloneSnapshot(b.state)
	endpoint.Modes = append([]string(nil), endpoint.Modes...)
	next.Endpoints[endpoint.ID] = endpoint
	if _, exists := next.Sessions[endpoint.ID]; !exists {
		next.Sessions[endpoint.ID] = Session{State: "unknown"}
	}
	return b.commit(next)
}

func (b *Broker) SetLeader(endpoint string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.writable(); err != nil {
		return err
	}
	registered, ok := b.state.Endpoints[endpoint]
	if !ok || !canReceiveResults(registered) {
		return errors.New("leader must be enabled, support queue, and have a verified idle or busy result-return path")
	}
	if b.state.Leader == endpoint {
		return nil
	}
	next := cloneSnapshot(b.state)
	next.Leader = endpoint
	next.LeaderEpoch++
	return b.commit(next)
}

func (b *Broker) Submit(submission Submission) (Task, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.writable(); err != nil {
		return Task{}, err
	}
	if old, exists := b.state.Tasks[submission.ID]; exists {
		if submission.Origin == "" {
			submission.Origin = old.Origin
		}
		if !sameJSON(submission, old.Submission) {
			return Task{}, errors.New("task ID was already used with different content")
		}
		return cloneTask(old), nil
	}
	if submission.Origin == "" {
		submission.Origin = b.state.Leader
	}
	if strings.TrimSpace(submission.ID) == "" || strings.TrimSpace(submission.Goal) == "" {
		return Task{}, errors.New("task ID and goal are required")
	}
	for label, value := range map[string]string{"task ID": submission.ID, "origin": submission.Origin, "recipient": submission.Recipient} {
		if err := checkIdentifier(label, value); err != nil {
			return Task{}, err
		}
	}
	if len(submission.Goal) > 64<<10 || len(submission.Context) > 64<<10 {
		return Task{}, errors.New("goal and context are limited to 64 KiB each")
	}
	if len(submission.Resources) > 64 || len(submission.AuthorizedActions) > 64 {
		return Task{}, errors.New("resources and authorized actions are limited to 64 each")
	}
	if len(submission.Inputs) > 64 {
		return Task{}, errors.New("input files are limited to 64")
	}
	for _, input := range submission.Inputs {
		if _, err := b.verifyArtifact(input); err != nil {
			return Task{}, fmt.Errorf("input file: %w", err)
		}
	}
	for _, action := range submission.AuthorizedActions {
		if strings.TrimSpace(action) == "" || len(action) > 512 {
			return Task{}, errors.New("authorized action must be nonempty and at most 512 bytes")
		}
	}
	if submission.MaxHandoffs != 1 {
		return Task{}, errors.New("V0 requires max_handoffs to be exactly 1")
	}
	if submission.ParentTaskID != "" || submission.OriginRef != "" || submission.MaxHops != 0 {
		return Task{}, errors.New("legacy task submission does not support archived peer workflow fields; use addressed mail")
	}
	if submission.Deadline == "" {
		return Task{}, errors.New("deadline is required")
	}
	if _, err := time.Parse(time.RFC3339, submission.Deadline); err != nil {
		return Task{}, fmt.Errorf("deadline must be RFC3339: %w", err)
	}
	if !validMode(submission.Mode) {
		return Task{}, errors.New("unsupported delivery mode")
	}
	if origin, ok := b.state.Endpoints[submission.Origin]; !ok || !canReceiveResults(origin) {
		return Task{}, errors.New("origin must be enabled, support queue, and have a verified idle or busy result-return path")
	}
	recipient, ok := b.state.Endpoints[submission.Recipient]
	if !ok || !recipient.Enabled {
		return Task{}, errors.New("recipient must be registered and enabled")
	}
	if !supports(recipient, submission.Mode) {
		return Task{}, errors.New("recipient does not support requested mode; no fallback was applied")
	}
	if submission.Mode == "queue" && submission.TargetRunID != "" {
		return Task{}, errors.New("queue tasks cannot target an existing run")
	}
	if submission.Mode != "queue" {
		if submission.TargetRunID == "" {
			return Task{}, errors.New("control messages require target_run_id")
		}
		if err := checkIdentifier("target run ID", submission.TargetRunID); err != nil {
			return Task{}, err
		}
		if _, ok := activeTarget(b.state, submission.Recipient, submission.TargetRunID); !ok {
			return Task{}, errors.New("target run is not an active managed task in the recipient session")
		}
		if len(submission.Resources) > 0 || submission.RequireArtifact {
			return Task{}, errors.New("control messages reuse the target run and cannot claim resources or require artifacts")
		}
	}
	resourceSet := map[string]bool{}
	for _, resource := range submission.Resources {
		if strings.TrimSpace(resource) == "" || len(resource) > 512 || resourceSet[resource] {
			return Task{}, errors.New("resources must be nonempty and unique")
		}
		resourceSet[resource] = true
	}
	submission.Resources = append([]string(nil), submission.Resources...)
	submission.Inputs = append([]Artifact(nil), submission.Inputs...)
	submission.AuthorizedActions = append([]string(nil), submission.AuthorizedActions...)
	coordinator := b.state.Leader
	if coordinator == "" {
		coordinator = submission.Origin
	}
	task := Task{Submission: submission, Coordinator: coordinator, LeaderEpoch: b.state.LeaderEpoch, Execution: "waiting"}
	if submission.Mode != "queue" {
		task.RunID = submission.TargetRunID
	}
	content, err := json.Marshal(submission)
	if err != nil {
		return Task{}, err
	}
	kind := "task"
	if submission.Mode != "queue" {
		kind = "control"
	}
	envelope := Envelope{ID: taskMessageID(submission.ID), TaskID: submission.ID, CorrelationID: submission.ID, Kind: kind, Sender: submission.Origin, Recipient: submission.Recipient, Mode: submission.Mode, TargetRunID: submission.TargetRunID, Content: string(content), Delivery: "pending"}
	envelope.CommunicationLanguage = collaborationLanguage(b.state.Endpoints[submission.Origin], recipient)
	next := cloneSnapshot(b.state)
	next.Tasks[task.ID] = task
	next.Messages[envelope.ID] = envelope
	if err = b.commit(next); err != nil {
		return Task{}, err
	}
	return cloneTask(task), nil
}

func (b *Broker) Next(endpoint string) (*Envelope, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.writable(); err != nil {
		return nil, err
	}
	registered, exists := b.state.Endpoints[endpoint]
	if !exists {
		return nil, errors.New("endpoint is not registered")
	}
	queueBlocked := false
	for _, id := range b.order {
		envelope := b.state.Messages[id]
		if envelope.Recipient != endpoint || envelope.Delivery != "pending" {
			continue
		}
		if envelope.Mailbox || envelope.RootOrigin != nil {
			continue
		}
		task, exists := b.state.Tasks[envelope.TaskID]
		if !exists {
			return nil, errors.New("message references missing task")
		}
		if envelope.Kind != "result" {
			deadline, err := time.Parse(time.RFC3339, task.Deadline)
			if err != nil {
				return nil, errors.New("stored task deadline is invalid")
			}
			if !time.Now().Before(deadline) {
				next := cloneSnapshot(b.state)
				envelope.Delivery = "failed"
				envelope.Detail = "deadline expired before dispatch"
				next.Messages[id] = envelope
				task.Execution = "failed"
				task.Summary = envelope.Detail
				next.Tasks[task.ID] = task
				addResult(&next, task)
				if err = b.commit(next); err != nil {
					return nil, err
				}
				return nil, errors.New(envelope.Detail)
			}
		}
		if !registered.Enabled || !supports(registered, envelope.Mode) {
			continue
		}
		if envelope.Mode == "queue" {
			session := b.state.Sessions[endpoint]
			busyResult := envelope.Kind == "result" && registered.ResultQueueWhileBusyVerified && (session.State == "busy" || session.State == "needs_user")
			if busyResult {
				// Results may enter a verified native busy return path. An unresolved
				// transport attempt still reserves the endpoint; business resource
				// locks and ordinary queued work are unaffected.
				if deliveryInFlight(b.state, endpoint) {
					queueBlocked = true
					continue
				}
			} else {
				if queueBlocked {
					continue
				}
				if !registered.IdleWakeVerified || session.State != "idle" || endpointBusy(b.state, endpoint) {
					queueBlocked = true
					continue
				}
			}
			conflict := false
			if envelope.Kind != "result" {
				for _, resource := range task.Resources {
					if owner := b.state.ResourceOwners[resource]; owner != "" && owner != task.ID {
						conflict = true
						break
					}
				}
			}
			if conflict {
				queueBlocked = true
				continue
			}
		} else {
			if _, ok := activeTarget(b.state, endpoint, envelope.TargetRunID); !ok {
				if _, recoverable := managedTarget(b.state, endpoint, envelope.TargetRunID); recoverable {
					continue
				}
				next := cloneSnapshot(b.state)
				envelope.Delivery = "failed"
				envelope.Detail = "control target is no longer an active matching run"
				next.Messages[id] = envelope
				task.Execution = "failed"
				task.Summary = envelope.Detail
				next.Tasks[task.ID] = task
				addResult(&next, task)
				if err := b.commit(next); err != nil {
					return nil, err
				}
				return nil, errors.New("control target is no longer an active matching run")
			}
			if controlInFlight(b.state, endpoint, envelope.TargetRunID) {
				continue
			}
		}
		next := cloneSnapshot(b.state)
		envelope.Delivery = "dispatching"
		next.Messages[id] = envelope
		if envelope.Kind == "task" {
			for _, resource := range task.Resources {
				next.ResourceOwners[resource] = task.ID
			}
		}
		if err := b.commit(next); err != nil {
			return nil, err
		}
		return &envelope, nil
	}
	return nil, nil
}

func (b *Broker) Delivery(messageID, outcome, detail string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.writable(); err != nil {
		return err
	}
	if len(detail) > 4096 {
		return errors.New("delivery detail is limited to 4 KiB")
	}
	if outcome != "delivered" && outcome != "uncertain" && outcome != "failed" {
		return errors.New("delivery outcome must be delivered, uncertain, or failed")
	}
	envelope, exists := b.state.Messages[messageID]
	if !exists {
		return errors.New("message is not registered")
	}
	if envelope.Delivery == "pending" {
		return errors.New("message has not been dispatched")
	}
	if envelope.Delivery == outcome && envelope.Detail == detail {
		return nil
	}
	if envelope.Mailbox {
		if envelope.Delivery == "delivered" && outcome != "delivered" {
			return errors.New("confirmed mail delivery cannot become uncertain or failed")
		}
		if envelope.Delivery == "failed" && outcome != "failed" {
			return errors.New("failed mail delivery cannot be retried through a receipt")
		}
		if envelope.Delivery == "uncertain" && outcome == "failed" {
			return errors.New("uncertain mail requires delivery reconciliation, not a failure retry")
		}
		next := cloneSnapshot(b.state)
		envelope.Delivery, envelope.Detail = outcome, detail
		next.Messages[messageID] = envelope
		return b.commit(next)
	}
	if envelope.Delivery == "delivered" && outcome != "delivered" {
		// The native assistant can report while its transport process is still
		// running. Its accepted execution is stronger evidence than that process's
		// late timeout or exit status. Only that task recipient's run-backed report
		// qualifies; a transport-only delivery or a result receipt still conflicts.
		task, exists := b.state.Tasks[envelope.TaskID]
		if exists && envelope.Kind != "result" && envelope.ID == taskMessageID(task.ID) && task.Recipient == envelope.Recipient && task.RunID != "" && validExecutionReport(task.Execution) {
			return nil
		}
		return errors.New("confirmed delivery cannot become uncertain or failed")
	}
	if envelope.Delivery == "failed" && outcome != "failed" {
		return errors.New("failed delivery cannot be retried through a receipt")
	}
	if envelope.Delivery == "uncertain" && outcome == "failed" {
		return errors.New("uncertain delivery requires execution reconciliation, not a failure retry")
	}
	next := cloneSnapshot(b.state)
	envelope.Delivery = outcome
	envelope.Detail = detail
	next.Messages[messageID] = envelope
	if outcome == "failed" && envelope.Kind != "result" {
		task := next.Tasks[envelope.TaskID]
		if task.Execution != "waiting" {
			return errors.New("delivery failure cannot cancel an already accepted task")
		}
		task.Execution = "failed"
		task.Summary = detail
		if task.Summary == "" {
			task.Summary = "delivery failed before acceptance"
		}
		next.Tasks[task.ID] = task
		releaseResources(&next, task.ID)
		addResult(&next, task)
	}
	return b.commit(next)
}

func (b *Broker) SetSession(endpoint, state, runID string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.writable(); err != nil {
		return err
	}
	if runID != "" {
		if err := checkIdentifier("run ID", runID); err != nil {
			return err
		}
	}
	if _, ok := b.state.Endpoints[endpoint]; !ok {
		return errors.New("endpoint is not registered")
	}
	if state != "unknown" && state != "idle" && state != "busy" && state != "needs_user" {
		return errors.New("invalid native session state")
	}
	if state == "idle" && runID != "" {
		return errors.New("idle session cannot have a run ID")
	}
	if (state == "busy" || state == "needs_user") && runID == "" {
		return errors.New("active native session requires run ID")
	}
	for _, task := range b.state.Tasks {
		if task.RootOrigin == nil && task.Recipient == endpoint && task.Mode == "queue" && !terminal(task.Execution) && task.RunID != "" {
			if state == "idle" || (runID != "" && runID != task.RunID) {
				return errors.New("unfinished managed run must be reconciled by an execution report")
			}
		}
	}
	next := cloneSnapshot(b.state)
	next.Sessions[endpoint] = Session{State: state, RunID: runID}
	return b.commit(next)
}

func (b *Broker) Report(report Report) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.writable(); err != nil {
		return err
	}
	if report.ResumeMessageID != "" {
		return errors.New("archived peer resume reports are unsupported; use addressed mail")
	}
	if strings.TrimSpace(report.EventID) == "" {
		return errors.New("event ID is required")
	}
	for label, value := range map[string]string{"event ID": report.EventID, "task ID": report.TaskID, "report endpoint": report.Endpoint} {
		if err := checkIdentifier(label, value); err != nil {
			return err
		}
	}
	if report.RunID != "" {
		if err := checkIdentifier("run ID", report.RunID); err != nil {
			return err
		}
	}
	if len(report.Summary) > 64<<10 || len(report.Artifacts) > 64 {
		return errors.New("report summary is limited to 64 KiB and artifacts to 64")
	}
	encoded, err := json.Marshal(report)
	if err != nil {
		return err
	}
	fingerprint := sha256.Sum256(encoded)
	signature := hex.EncodeToString(fingerprint[:])
	if previous, exists := b.state.EventIDs[report.EventID]; exists {
		if previous == signature {
			return nil
		}
		return errors.New("event ID was already used with a different payload")
	}
	task, exists := b.state.Tasks[report.TaskID]
	if exists && task.RootOrigin != nil {
		return errors.New("archived peer prototype records are read-only; use addressed mail")
	}
	if !exists || task.Recipient != report.Endpoint {
		return errors.New("report must come from the task recipient")
	}
	if !validExecutionReport(report.State) {
		return errors.New("invalid execution report state")
	}
	if terminal(task.Execution) {
		return errors.New("task execution is already terminal")
	}
	envelope := b.state.Messages[taskMessageID(task.ID)]
	if envelope.Delivery == "pending" {
		return errors.New("undispatched task cannot report execution")
	}
	if report.RunID == "" {
		report.RunID = task.RunID
	}
	if task.Mode == "queue" {
		if (report.State == "accepted" || report.State == "running" || report.State == "needs_user") && report.RunID == "" {
			return errors.New("execution acceptance requires a run ID")
		}
		if task.RunID != "" && report.RunID != task.RunID {
			return errors.New("report run ID does not match the accepted run")
		}
		if task.RunID == "" && report.RunID != "" {
			for _, other := range b.state.Tasks {
				if other.ID != task.ID && other.Recipient == task.Recipient && other.Mode == "queue" && other.RunID == report.RunID {
					return errors.New("run ID is already owned by another task")
				}
			}
		}
	} else if report.RunID != task.TargetRunID {
		return errors.New("control report must reference its target run")
	}
	if !allowedTransition(task.Execution, report.State) {
		return errors.New("execution report would move the task backwards")
	}
	if task.Mode == "interrupt" && report.State == "succeeded" {
		return errors.New("interrupt requires an actual interrupted receipt")
	}
	if task.Mode == "steer" && report.State == "interrupted" {
		return errors.New("steer cannot interrupt the target run")
	}
	artifacts := append([]Artifact(nil), report.Artifacts...)
	if report.State == "succeeded" {
		if strings.TrimSpace(report.Summary) == "" {
			return errors.New("successful execution requires a summary")
		}
		if task.RequireArtifact && len(artifacts) == 0 {
			return errors.New("successful execution requires at least one artifact")
		}
		for i, artifact := range artifacts {
			artifacts[i], err = b.verifyArtifact(artifact)
			if err != nil {
				return err
			}
		}
	}
	next := cloneSnapshot(b.state)
	task.Execution = report.State
	task.RunID = report.RunID
	task.Summary = report.Summary
	task.Artifacts = artifacts
	next.Tasks[task.ID] = task
	envelope.Delivery = "delivered"
	envelope.Detail = "execution receipt confirms native session acceptance"
	next.Messages[envelope.ID] = envelope
	if task.Mode == "queue" {
		if terminal(report.State) {
			releaseResources(&next, task.ID)
			next.Sessions[task.Recipient] = Session{State: "idle"}
			addResult(&next, task)
		} else {
			state := "busy"
			if report.State == "needs_user" {
				state = "needs_user"
			}
			next.Sessions[task.Recipient] = Session{State: state, RunID: task.RunID}
		}
		if report.State == "interrupted" {
			resolveInterruptControls(&next, task)
		}
	} else {
		if task.Mode == "interrupt" && report.State == "interrupted" {
			original, ok := managedTarget(next, task.Recipient, task.TargetRunID)
			if !ok {
				return errors.New("interrupt target is not an active matching run")
			}
			original.Execution = "interrupted"
			original.Summary = report.Summary
			next.Tasks[original.ID] = original
			releaseResources(&next, original.ID)
			next.Sessions[task.Recipient] = Session{State: "idle"}
			addResult(&next, original)
			resolveInterruptControls(&next, original)
		}
		if terminal(report.State) {
			addResult(&next, next.Tasks[task.ID])
		}
	}
	next.EventIDs[report.EventID] = signature
	return b.commit(next)
}

func (b *Broker) verifyArtifact(artifact Artifact) (Artifact, error) {
	if len(artifact.Path) > 4096 || len(artifact.SHA256) > 64 {
		return Artifact{}, errors.New("artifact path or checksum exceeds its size limit")
	}
	if artifact.DeviceID != b.deviceID {
		return Artifact{}, errors.New("artifact belongs to another device; local paths are not transferable")
	}
	root, ok := b.workspaces[artifact.WorkspaceID]
	if !ok {
		return Artifact{}, errors.New("artifact workspace is not allowlisted")
	}
	path := artifact.Path
	if path == "" || strings.Contains(path, "\\") || filepath.IsAbs(path) || filepath.VolumeName(path) != "" || (len(path) > 1 && path[1] == ':') {
		return Artifact{}, errors.New("artifact requires a portable relative path")
	}
	clean := filepath.Clean(filepath.FromSlash(path))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return Artifact{}, errors.New("artifact path escapes its workspace")
	}
	resolved, err := filepath.EvalSymlinks(filepath.Join(root, clean))
	if err != nil {
		return Artifact{}, fmt.Errorf("artifact file is unavailable: %w", err)
	}
	if !withinRoot(root, resolved) {
		return Artifact{}, errors.New("artifact symlink escapes its workspace")
	}
	file, err := os.Open(resolved)
	if err != nil {
		return Artifact{}, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return Artifact{}, err
	}
	if !info.Mode().IsRegular() {
		return Artifact{}, errors.New("artifact must be a regular file")
	}
	hash := sha256.New()
	if _, err = io.Copy(hash, file); err != nil {
		return Artifact{}, err
	}
	checksum := hex.EncodeToString(hash.Sum(nil))
	if artifact.SHA256 != "" && !strings.EqualFold(artifact.SHA256, checksum) {
		return Artifact{}, errors.New("artifact SHA256 does not match")
	}
	finalPath, err := filepath.EvalSymlinks(filepath.Join(root, clean))
	if err != nil || finalPath != resolved || !withinRoot(root, finalPath) {
		return Artifact{}, errors.New("artifact path changed during verification")
	}
	finalInfo, err := os.Stat(finalPath)
	if err != nil || !os.SameFile(info, finalInfo) {
		return Artifact{}, errors.New("artifact file changed during verification")
	}
	artifact.Path = filepath.ToSlash(clean)
	artifact.SHA256 = checksum
	return artifact, nil
}

func withinRoot(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}
func validMode(mode string) bool { return mode == "queue" || mode == "steer" || mode == "interrupt" }
func supports(endpoint Endpoint, mode string) bool {
	for _, supported := range endpoint.Modes {
		if supported == mode {
			return true
		}
	}
	return false
}
func canReceiveResults(endpoint Endpoint) bool {
	return endpoint.Enabled && supports(endpoint, "queue") && (endpoint.IdleWakeVerified || endpoint.ResultQueueWhileBusyVerified)
}
func terminal(state string) bool {
	return state == "succeeded" || state == "failed" || state == "interrupted"
}
func validExecutionReport(state string) bool {
	return state == "accepted" || state == "running" || state == "needs_user" || terminal(state)
}
func allowedTransition(from, to string) bool {
	if from == "waiting" {
		return true
	}
	if from == "accepted" {
		return to != "waiting"
	}
	if from == "running" {
		return to != "waiting" && to != "accepted"
	}
	if from == "needs_user" {
		return to != "waiting" && to != "accepted"
	}
	return false
}
func taskMessageID(id string) string   { return "task:" + id }
func resultMessageID(id string) string { return "result:" + id }
func releaseResources(state *Snapshot, taskID string) {
	for resource, owner := range state.ResourceOwners {
		if owner == taskID {
			delete(state.ResourceOwners, resource)
		}
	}
}
func activeTarget(state Snapshot, endpoint, runID string) (Task, bool) {
	session := state.Sessions[endpoint]
	if runID == "" || session.RunID != runID || (session.State != "busy" && session.State != "needs_user") {
		return Task{}, false
	}
	return managedTarget(state, endpoint, runID)
}

func managedTarget(state Snapshot, endpoint, runID string) (Task, bool) {
	if runID == "" {
		return Task{}, false
	}
	for _, task := range state.Tasks {
		if task.Recipient == endpoint && task.Mode == "queue" && task.RunID == runID && (task.Execution == "accepted" || task.Execution == "running" || task.Execution == "needs_user") {
			return task, true
		}
	}
	return Task{}, false
}
func endpointBusy(state Snapshot, endpoint string) bool {
	for _, task := range state.Tasks {
		if task.Recipient != endpoint || task.Mode != "queue" || terminal(task.Execution) {
			continue
		}
		if task.Execution != "waiting" {
			return true
		}
		message := state.Messages[taskMessageID(task.ID)]
		if message.Delivery == "dispatching" || message.Delivery == "uncertain" || message.Delivery == "delivered" {
			return true
		}
	}
	return deliveryInFlight(state, endpoint)
}

func deliveryInFlight(state Snapshot, endpoint string) bool {
	for _, message := range state.Messages {
		if message.Recipient == endpoint && (message.Delivery == "dispatching" || message.Delivery == "uncertain") {
			return true
		}
	}
	return false
}
func controlInFlight(state Snapshot, endpoint, runID string) bool {
	for _, task := range state.Tasks {
		if task.Recipient == endpoint && task.Mode != "queue" && task.TargetRunID == runID && !terminal(task.Execution) {
			message := state.Messages[taskMessageID(task.ID)]
			if message.Delivery != "pending" && message.Delivery != "failed" {
				return true
			}
		}
	}
	return false
}
func addResult(state *Snapshot, task Task) {
	id := resultMessageID(task.ID)
	if _, exists := state.Messages[id]; exists {
		return
	}
	content, _ := json.Marshal(task)
	state.Messages[id] = Envelope{ID: id, TaskID: task.ID, ReplyTo: taskMessageID(task.ID), CorrelationID: task.ID, Kind: "result", Sender: task.Recipient, Recipient: task.Origin, Mode: "queue", Content: string(content), Delivery: "pending",
		CommunicationLanguage: collaborationLanguage(state.Endpoints[task.Recipient], state.Endpoints[task.Origin])}
}
func resolveInterruptControls(state *Snapshot, original Task) {
	for id, task := range state.Tasks {
		if task.Mode == "interrupt" && task.Recipient == original.Recipient && task.TargetRunID == original.RunID && !terminal(task.Execution) {
			message := state.Messages[taskMessageID(id)]
			if message.Delivery == "pending" {
				continue
			}
			task.Execution = "interrupted"
			task.Summary = original.Summary
			state.Tasks[id] = task
			message.Delivery = "delivered"
			message.Detail = "target run confirmed interrupted"
			state.Messages[message.ID] = message
			addResult(state, task)
		}
	}
}
func sortedMessageIDs(messages map[string]Envelope) []string {
	ids := make([]string, 0, len(messages))
	for id := range messages {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
func sameJSON(left, right any) bool {
	a, _ := json.Marshal(left)
	b, _ := json.Marshal(right)
	return string(a) == string(b)
}
func cloneTask(task Task) Task {
	if task.RootOrigin != nil {
		origin := *task.RootOrigin
		task.RootOrigin = &origin
	}
	task.Inputs = append([]Artifact(nil), task.Inputs...)
	task.Resources = append([]string(nil), task.Resources...)
	task.AuthorizedActions = append([]string(nil), task.AuthorizedActions...)
	task.Artifacts = append([]Artifact(nil), task.Artifacts...)
	return task
}
func cloneSnapshot(original Snapshot) Snapshot {
	next := Snapshot{Version: original.Version, Leader: original.Leader, LeaderEpoch: original.LeaderEpoch, Endpoints: make(map[string]Endpoint, len(original.Endpoints)), Sessions: make(map[string]Session, len(original.Sessions)), Tasks: make(map[string]Task, len(original.Tasks)), Messages: make(map[string]Envelope, len(original.Messages)), EventIDs: make(map[string]string, len(original.EventIDs)), ResourceOwners: make(map[string]string, len(original.ResourceOwners)), Collaborations: make(map[string]Collaboration, len(original.Collaborations)), MailConversations: make(map[string]MailConversation, len(original.MailConversations))}
	for id, endpoint := range original.Endpoints {
		endpoint.Modes = append([]string(nil), endpoint.Modes...)
		next.Endpoints[id] = endpoint
	}
	for id, session := range original.Sessions {
		next.Sessions[id] = session
	}
	for id, task := range original.Tasks {
		next.Tasks[id] = cloneTask(task)
	}
	for id, message := range original.Messages {
		next.Messages[id] = cloneEnvelope(message)
	}
	for id, event := range original.EventIDs {
		next.EventIDs[id] = event
	}
	for id, owner := range original.ResourceOwners {
		next.ResourceOwners[id] = owner
	}
	for id, conversation := range original.Collaborations {
		next.Collaborations[id] = conversation
	}
	for id, conversation := range original.MailConversations {
		if conversation.Federation != nil {
			federation := *conversation.Federation
			conversation.Federation = &federation
		}
		conversation.Participants = cloneParticipants(conversation.Participants)
		next.MailConversations[id] = conversation
	}
	return next
}

func checkIdentifier(label, value string) error {
	if strings.TrimSpace(value) == "" || len(value) > 128 {
		return fmt.Errorf("%s must be nonempty and at most 128 bytes", label)
	}
	return nil
}
