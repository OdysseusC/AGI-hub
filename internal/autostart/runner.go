package autostart

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

// ErrHubUnavailable is the only probe error that permits starting a Hub. A
// listener that rejects authentication or is not the expected local Hub must
// return a different error, so startup fails closed without opening a second Hub.
var ErrHubUnavailable = errors.New("the local Hub listener is unavailable")

var ErrAlreadyRunning = errors.New("a login startup runner already owns this data directory")

type RunOptions struct {
	Probe      func(context.Context) error
	ClientGate func(Client) error
}

// RuntimeState is a local process observation, not a native assistant readiness
// claim. PID identifies the runner that last wrote this snapshot and can be stale.
type RuntimeState struct {
	PID        int              `json:"pid"`
	StartedAt  time.Time        `json:"started_at"`
	UpdatedAt  time.Time        `json:"updated_at"`
	Components []ComponentState `json:"components"`
}

type ComponentState struct {
	ID       string `json:"id"`
	State    string `json:"state"`
	Detail   string `json:"detail"`
	PID      int    `json:"pid"`
	Attempts int    `json:"attempts"`
}

func ReadRuntime(dir string) (RuntimeState, error) {
	var state RuntimeState
	err := readJSON(filepath.Join(dir, "startup-state.json"), &state)
	if errors.Is(err, os.ErrNotExist) {
		return RuntimeState{}, nil
	}
	return state, err
}

type runPolicy struct {
	probeInterval time.Duration
	probeTimeout  time.Duration
	readyTimeout  time.Duration
	stopGrace     time.Duration
	restartDelays []time.Duration
}

func defaultRunPolicy() runPolicy {
	return runPolicy{
		probeInterval: 500 * time.Millisecond,
		probeTimeout:  2 * time.Second,
		readyTimeout:  10 * time.Second,
		stopGrace:     3 * time.Second,
		restartDelays: []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second},
	}
}

type startupRunner struct {
	dir      string
	plan     Plan
	options  RunOptions
	policy   runPolicy
	state    RuntimeState
	logger   *log.Logger
	exits    chan *ownedProcess
	owned    []*ownedProcess
	hub      *ownedProcess
	attempts int
	clients  map[string]*clientRun
}

type clientRun struct {
	client   Client
	attempts int
	process  *ownedProcess
	retryAt  time.Time
}

// Run is a foreground login runner. It registers no OS trigger and starts
// nothing when the saved plan is disabled. Cancellation stops only children
// created by this invocation; an already authenticated Hub is only attached.
func Run(ctx context.Context, dir string, plan Plan, options RunOptions) error {
	return runWithPolicy(ctx, dir, plan, options, defaultRunPolicy())
}

func runWithPolicy(ctx context.Context, dir string, plan Plan, options RunOptions, policy runPolicy) (runErr error) {
	if !plan.Enabled {
		return nil
	}
	if err := Validate(plan); err != nil {
		return err
	}
	if !filepath.IsAbs(dir) {
		return errors.New("login startup requires an absolute data directory")
	}
	if options.Probe == nil {
		return errors.New("login startup requires an authenticated local Hub readiness probe")
	}
	if err := ctx.Err(); err != nil {
		return nil
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create login startup data directory: %w", err)
	}
	lock, err := acquireRuntimeLock(dir)
	if err != nil {
		return err
	}
	defer lock.Close()
	logDir := filepath.Join(dir, "logs", "autostart")
	if err := os.MkdirAll(logDir, 0700); err != nil {
		return fmt.Errorf("create login startup log directory: %w", err)
	}
	logFile, err := openRuntimeLog(filepath.Join(logDir, "runner.log"))
	if err != nil {
		return fmt.Errorf("open private login startup log: %w", err)
	}
	defer logFile.Close()
	now := time.Now().UTC()
	r := &startupRunner{
		dir: dir, plan: plan, options: options, policy: policy,
		state:   RuntimeState{PID: os.Getpid(), StartedAt: now, UpdatedAt: now, Components: []ComponentState{}},
		logger:  log.New(logFile, "", log.Ldate|log.Ltime|log.LUTC),
		exits:   make(chan *ownedProcess, len(plan.Clients)+len(policy.restartDelays)+2),
		clients: make(map[string]*clientRun),
	}
	// The kernel lock can already be observed by the UI. Replace an earlier
	// runner's snapshot before probing, so its stale children cannot briefly
	// appear to belong to this invocation while the first probe is pending.
	if err := writeJSON(filepath.Join(dir, "startup-state.json"), r.state); err != nil {
		return fmt.Errorf("initialize login startup state: %w", err)
	}
	defer func() {
		for i := len(r.owned) - 1; i >= 0; i-- {
			child := r.owned[i]
			child.stop(policy.stopGrace)
			component := r.component(child.id)
			// Keep failure, policy gates and the final retry observation visible.
			// An attached Hub is absent from owned and is never stopped or changed.
			if component != nil && component.PID == child.cmd.Process.Pid && (component.State == "running" || component.State == "hub_ready" || component.State == "backoff") {
				if err := r.set(child.id, "stopped", "owned process stopped", 0, component.Attempts); err != nil {
					runErr = errors.Join(runErr, err)
				}
			}
		}
		for _, component := range r.state.Components {
			if component.State == "backoff" && component.PID == 0 {
				if err := r.set(component.ID, "stopped", "startup runner stopped before a pending restart", 0, component.Attempts); err != nil {
					runErr = errors.Join(runErr, err)
				}
			}
		}
	}()
	if err := r.establishHub(ctx); err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return err
	}
	if err := r.startClients(ctx); err != nil {
		return err
	}
	return r.monitor(ctx)
}

func (r *startupRunner) component(id string) *ComponentState {
	for i := range r.state.Components {
		if r.state.Components[i].ID == id {
			return &r.state.Components[i]
		}
	}
	return nil
}

func (r *startupRunner) set(id, state, detail string, pid, attempts int) error {
	component := r.component(id)
	if component == nil {
		r.state.Components = append(r.state.Components, ComponentState{ID: id})
		component = &r.state.Components[len(r.state.Components)-1]
	}
	*component = ComponentState{ID: id, State: state, Detail: detail, PID: pid, Attempts: attempts}
	r.state.UpdatedAt = time.Now().UTC()
	if err := writeJSON(filepath.Join(r.dir, "startup-state.json"), r.state); err != nil {
		return fmt.Errorf("write login startup state: %w", err)
	}
	// Command lines, child output, environment values, endpoint names and raw
	// errors are deliberately absent. A child may print credentials at any time.
	kind := "client"
	if id == "hub" {
		kind = "hub"
	}
	r.logger.Printf("component=%s state=%s attempts=%d", kind, state, attempts)
	return nil
}

func (r *startupRunner) probe(ctx context.Context) error {
	probeCtx, cancel := context.WithTimeout(ctx, r.policy.probeTimeout)
	defer cancel()
	return r.options.Probe(probeCtx)
}

func (r *startupRunner) establishHub(ctx context.Context) error {
	err := r.probe(ctx)
	if err == nil {
		return r.set("hub", "hub_ready", "attached to an authenticated local Hub; process ownership is external", 0, 0)
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if !errors.Is(err, ErrHubUnavailable) {
		if stateErr := r.set("hub", "needs_user", "local Hub authentication or identity verification failed", 0, 0); stateErr != nil {
			return stateErr
		}
		return errors.New("local Hub verification failed; user action is required")
	}
	return r.startHub(ctx)
}

func (r *startupRunner) startHub(ctx context.Context) error {
	for r.attempts <= len(r.policy.restartDelays) {
		if r.attempts > 0 {
			if err := r.set("hub", "backoff", "waiting before a bounded Hub restart", 0, r.attempts); err != nil {
				return err
			}
			if err := waitRuntime(ctx, r.policy.restartDelays[r.attempts-1]); err != nil {
				return err
			}
			// A user may have started their own Hub during the backoff. Attach
			// when authenticated instead of racing another process into the port.
			err := r.probe(ctx)
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if err == nil {
				r.hub = nil
				return r.set("hub", "hub_ready", "attached to an authenticated local Hub; process ownership is external", 0, r.attempts)
			}
			if !errors.Is(err, ErrHubUnavailable) {
				if stateErr := r.set("hub", "needs_user", "local Hub authentication or identity verification failed", 0, r.attempts); stateErr != nil {
					return stateErr
				}
				return errors.New("local Hub verification failed; user action is required")
			}
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		r.attempts++
		cmd := exec.Command(r.plan.Binary, "serve", "--data", r.dir)
		cmd.Dir = r.dir
		cmd.Env = runtimeEnvironment(nil)
		child, err := r.start(cmd, "hub")
		if err != nil {
			if stateErr := r.set("hub", "failed", "Hub process could not be started", 0, r.attempts); stateErr != nil {
				return stateErr
			}
			continue
		}
		r.hub = child
		if err := r.set("hub", "backoff", "waiting for authenticated local Hub readiness", child.cmd.Process.Pid, r.attempts); err != nil {
			return err
		}
		err = r.waitReady(ctx, child)
		if err == nil {
			return r.set("hub", "hub_ready", "owned local Hub authenticated and ready", child.cmd.Process.Pid, r.attempts)
		}
		child.stop(r.policy.stopGrace)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if !errors.Is(err, ErrHubUnavailable) {
			if stateErr := r.set("hub", "needs_user", "local Hub authentication or identity verification failed", 0, r.attempts); stateErr != nil {
				return stateErr
			}
			return errors.New("local Hub verification failed; user action is required")
		}
		if err := r.set("hub", "failed", "Hub exited or did not become ready", 0, r.attempts); err != nil {
			return err
		}
	}
	return errors.New("login startup exhausted its bounded Hub restart attempts")
}

func (r *startupRunner) waitReady(ctx context.Context, child *ownedProcess) error {
	readyCtx, cancel := context.WithTimeout(ctx, r.policy.readyTimeout)
	defer cancel()
	for {
		select {
		case <-child.done:
			return ErrHubUnavailable
		default:
		}
		err := r.probe(readyCtx)
		if err == nil {
			return nil
		}
		if readyCtx.Err() != nil {
			return ErrHubUnavailable
		}
		if !errors.Is(err, ErrHubUnavailable) {
			return err
		}
		timer := time.NewTimer(r.policy.probeInterval)
		select {
		case <-readyCtx.Done():
			timer.Stop()
			return ErrHubUnavailable
		case <-child.done:
			timer.Stop()
			return ErrHubUnavailable
		case <-timer.C:
		}
	}
}

func (r *startupRunner) startClients(ctx context.Context) error {
	for _, client := range r.plan.Clients {
		if !client.Enabled {
			continue
		}
		if ctx.Err() != nil {
			return nil
		}
		clientState := &clientRun{client: client}
		r.clients["client:"+client.Endpoint] = clientState
		if err := r.startClient(ctx, clientState); err != nil {
			return err
		}
	}
	return nil
}

func (r *startupRunner) startClient(ctx context.Context, clientState *clientRun) error {
	clientState.retryAt = time.Time{}
	if ctx.Err() != nil {
		return nil
	}
	client := clientState.client
	id := "client:" + client.Endpoint
	if client.RequiresHost {
		return r.set(id, "needs_host_environment", "start this Client from its native host environment", 0, clientState.attempts)
	}
	if r.options.ClientGate != nil && r.options.ClientGate(client) != nil {
		return r.set(id, "needs_user", "Client startup prerequisites require user action", 0, clientState.attempts)
	}
	clientState.attempts++
	cmd := exec.Command(client.Command, client.Args...)
	cmd.Dir = client.Cwd
	cmd.Env = runtimeEnvironment(client.Environment)
	child, err := r.start(cmd, id)
	if err != nil {
		return r.clientFailed(clientState)
	}
	clientState.process = child
	return r.set(id, "running", "process started; native assistant readiness is not verified", child.cmd.Process.Pid, clientState.attempts)
}

func (r *startupRunner) clientFailed(client *clientRun) error {
	id := "client:" + client.client.Endpoint
	if client.attempts > len(r.policy.restartDelays) {
		return r.set(id, "failed", "Client exhausted its bounded restart attempts", 0, client.attempts)
	}
	client.retryAt = time.Now().Add(r.policy.restartDelays[client.attempts-1])
	return r.set(id, "backoff", "waiting before a bounded Client restart", 0, client.attempts)
}

func (r *startupRunner) retryClients(ctx context.Context) error {
	// Preserve the reviewed plan order on the initial launch and every retry.
	for _, client := range r.plan.Clients {
		state := r.clients["client:"+client.Endpoint]
		if state == nil || state.retryAt.IsZero() || time.Now().Before(state.retryAt) {
			continue
		}
		if err := r.startClient(ctx, state); err != nil {
			return err
		}
	}
	return nil
}

func (r *startupRunner) monitor(ctx context.Context) error {
	ticker := time.NewTicker(r.policy.probeInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case child := <-r.exits:
			if child.id == "hub" {
				if child != r.hub {
					continue
				}
				child.stop(r.policy.stopGrace)
				if err := r.set("hub", "failed", "owned Hub process exited", 0, r.attempts); err != nil {
					return err
				}
				if err := r.startHub(ctx); err != nil {
					if ctx.Err() != nil {
						return nil
					}
					return err
				}
				continue
			}
			client := r.clients[child.id]
			if client == nil || client.process != child {
				continue
			}
			child.stop(r.policy.stopGrace)
			client.process = nil
			if child.err != nil {
				if err := r.clientFailed(client); err != nil {
					return err
				}
				continue
			}
			if err := r.set(child.id, "stopped", "Client process exited", 0, client.attempts); err != nil {
				return err
			}
		case <-ticker.C:
			err := r.probe(ctx)
			if err == nil {
				if err := r.retryClients(ctx); err != nil {
					return err
				}
				continue
			}
			if ctx.Err() != nil {
				return nil
			}
			if !errors.Is(err, ErrHubUnavailable) {
				if stateErr := r.set("hub", "needs_user", "local Hub authentication or identity verification failed", 0, r.attempts); stateErr != nil {
					return stateErr
				}
				return errors.New("local Hub verification failed; user action is required")
			}
			if r.hub == nil {
				if stateErr := r.set("hub", "failed", "the externally owned Hub is no longer ready", 0, r.attempts); stateErr != nil {
					return stateErr
				}
				return errors.New("the attached local Hub is no longer ready")
			}
			if err := r.set("hub", "backoff", "waiting for authenticated local Hub readiness", r.hub.cmd.Process.Pid, r.attempts); err != nil {
				return err
			}
			err = r.waitReady(ctx, r.hub)
			if err == nil {
				if err := r.set("hub", "hub_ready", "owned local Hub authenticated and ready", r.hub.cmd.Process.Pid, r.attempts); err != nil {
					return err
				}
				continue
			}
			r.hub.stop(r.policy.stopGrace)
			if ctx.Err() != nil {
				return nil
			}
			if !errors.Is(err, ErrHubUnavailable) {
				if stateErr := r.set("hub", "needs_user", "local Hub authentication or identity verification failed", 0, r.attempts); stateErr != nil {
					return stateErr
				}
				return errors.New("local Hub verification failed; user action is required")
			}
			if err := r.set("hub", "failed", "owned Hub readiness was lost", 0, r.attempts); err != nil {
				return err
			}
			if err := r.startHub(ctx); err != nil {
				if ctx.Err() != nil {
					return nil
				}
				return err
			}
		}
	}
}

type ownedProcess struct {
	id    string
	cmd   *exec.Cmd
	done  chan struct{}
	err   error // Written before done is closed.
	guard processGuard
}

func (r *startupRunner) start(cmd *exec.Cmd, id string) (*ownedProcess, error) {
	cmd.Stdin = nil
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	configureOwnedCommand(cmd)
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	guard, err := guardOwnedProcess(cmd)
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, err
	}
	child := &ownedProcess{id: id, cmd: cmd, done: make(chan struct{}), guard: guard}
	r.owned = append(r.owned, child)
	go func() {
		child.err = cmd.Wait()
		close(child.done)
		r.exits <- child
	}()
	return child, nil
}

func (p *ownedProcess) stop(grace time.Duration) {
	p.guard.stop(p.cmd.Process, p.done, grace)
}

func waitRuntime(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// Inherit only OS location and locale settings. In particular, model keys,
// assistant IPC variables, loader flags and proxy settings do not cross from
// the current assistant into an unrelated startup process. Proxies must be
// explicitly present in the reviewed Client environment.
func runtimeEnvironment(explicit map[string]string) []string {
	allowed := map[string]bool{}
	for _, key := range []string{"PATH", "HOME", "USER", "USERNAME", "LOGNAME", "TMPDIR", "TMP", "TEMP", "LANG", "LC_ALL", "LC_CTYPE", "TZ", "SYSTEMROOT", "WINDIR", "USERPROFILE", "HOMEDRIVE", "HOMEPATH", "APPDATA", "LOCALAPPDATA", "SYSTEMDRIVE", "PATHEXT"} {
		allowed[key] = true
	}
	values := map[string]string{}
	keyFor := func(key string) string {
		if runtime.GOOS == "windows" {
			return strings.ToUpper(key)
		}
		return key
	}
	for _, entry := range os.Environ() {
		key, value, ok := strings.Cut(entry, "=")
		if ok && allowed[keyFor(key)] {
			values[keyFor(key)] = value
		}
	}
	for key, value := range explicit {
		values[keyFor(key)] = value
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	environment := make([]string, 0, len(values))
	for _, key := range keys {
		environment = append(environment, key+"="+values[key])
	}
	return environment
}
