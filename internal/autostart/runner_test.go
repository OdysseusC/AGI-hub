package autostart

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// Helpers are this test binary, never a real Hub or assistant. Hub argv is the
// exact production argv, so these tests also exercise executable/argument use.
func TestMain(m *testing.M) {
	if len(os.Args) == 4 && os.Args[1] == "serve" && os.Args[2] == "--data" {
		helperHub(os.Args[3])
		os.Exit(0)
	}
	if len(os.Args) >= 3 && os.Args[1] == "startup-test-client" {
		helperClient(os.Args[2:])
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func helperAppend(path string) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		os.Exit(91)
	}
	_, _ = fmt.Fprintln(file, "started")
	_ = file.Close()
}

func helperWait(ready, stopped string) {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)
	_ = os.WriteFile(ready, []byte("ready"), 0600)
	fmt.Fprintln(os.Stdout, "private-child-output runner-fake-secret")
	<-signals
	_ = os.Remove(ready)
	_ = os.WriteFile(stopped, []byte("stopped"), 0600)
}

func helperHub(dir string) {
	helperAppend(filepath.Join(dir, "helper-hub-count"))
	mode, _ := os.ReadFile(filepath.Join(dir, "helper-hub-mode"))
	if string(mode) == "fail" {
		os.Exit(7)
	}
	environment, _ := json.Marshal(os.Environ())
	_ = os.WriteFile(filepath.Join(dir, "helper-hub-environment.json"), environment, 0600)
	helperWait(filepath.Join(dir, "helper-hub-ready"), filepath.Join(dir, "helper-hub-stopped"))
}

func helperClient(args []string) {
	switch args[0] {
	case "fail":
		helperAppend(args[1])
		os.Exit(9)
	case "success":
		helperAppend(args[1])
	case "run":
		helperWait(args[1], args[2])
	case "tree":
		child := exec.Command(os.Args[0], "startup-test-client", "run", args[3], args[4])
		if err := child.Start(); err != nil {
			os.Exit(92)
		}
		_ = os.WriteFile(args[5], []byte(fmt.Sprint(child.Process.Pid)), 0600)
		helperWait(args[1], args[2])
		_ = child.Wait()
	default:
		os.Exit(93)
	}
}

func testRunPolicy() runPolicy {
	return runPolicy{
		probeInterval: 5 * time.Millisecond,
		probeTimeout:  time.Second,
		readyTimeout:  2 * time.Second,
		stopGrace:     250 * time.Millisecond,
		restartDelays: []time.Duration{time.Millisecond, 2 * time.Millisecond, 4 * time.Millisecond, 8 * time.Millisecond, 16 * time.Millisecond},
	}
}

func testPlan() Plan {
	return Plan{Version: 1, Enabled: true, Binary: os.Args[0], Clients: []Client{}}
}

func testClient(t *testing.T, endpoint string, args ...string) Client {
	t.Helper()
	return Client{Endpoint: endpoint, Enabled: true, Command: os.Args[0], Args: append([]string{"startup-test-client"}, args...), Cwd: t.TempDir()}
}

func waitRuntimeState(t *testing.T, dir string, done <-chan error, predicate func(RuntimeState) bool) RuntimeState {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		state, err := ReadRuntime(dir)
		if err != nil {
			t.Fatal(err)
		}
		if predicate(state) {
			return state
		}
		select {
		case err := <-done:
			t.Fatalf("startup runner exited before reaching the expected state: error=%v state=%+v", err, state)
		default:
		}
		time.Sleep(5 * time.Millisecond)
	}
	state, _ := ReadRuntime(dir)
	t.Fatalf("startup state did not meet condition: %+v", state)
	return RuntimeState{}
}

func hasComponent(state RuntimeState, id, want string, attempts int) bool {
	for _, component := range state.Components {
		if component.ID == id && component.State == want && (attempts < 0 || component.Attempts == attempts) {
			return true
		}
	}
	return false
}

func waitHelperFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("helper file was not created: %s", path)
}

func startTestRunner(t *testing.T, dir string, plan Plan, options RunOptions) (context.CancelFunc, <-chan error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	finished := make(chan struct{})
	go func() {
		done <- runWithPolicy(ctx, dir, plan, options, testRunPolicy())
		close(finished)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-finished:
			select {
			case err := <-done:
				if err != nil {
					t.Errorf("startup runner returned an unexpected error: %v", err)
				}
			default:
			}
		case <-time.After(8 * time.Second):
			t.Error("startup runner did not stop")
		}
	})
	return cancel, done
}

func TestRunDisabledDoesNothing(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "absent")
	called := false
	err := Run(context.Background(), dir, Plan{}, RunOptions{Probe: func(context.Context) error { called = true; return nil }})
	if err != nil || called {
		t.Fatalf("disabled runner performed work: called=%v err=%v", called, err)
	}
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("disabled runner created directory: %v", err)
	}
	state, err := ReadRuntime(dir)
	if err != nil || state.PID != 0 || len(state.Components) != 0 {
		t.Fatalf("missing runtime state: %+v %v", state, err)
	}
	if active, err := RuntimeActive(dir); active || err != nil {
		t.Fatalf("absent runtime was active: %v %v", active, err)
	}
}

func TestRunReplacesOldSnapshotBeforeInitialProbe(t *testing.T) {
	dir := t.TempDir()
	previous := RuntimeState{PID: 987654, Components: []ComponentState{{ID: "client:previous", State: "running", PID: 987653}}}
	if err := writeJSON(filepath.Join(dir, "startup-state.json"), previous); err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	cancel, done := startTestRunner(t, dir, testPlan(), RunOptions{Probe: func(ctx context.Context) error {
		close(entered)
		<-ctx.Done()
		return ctx.Err()
	}})
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("initial readiness probe was not reached")
	}
	state, err := ReadRuntime(dir)
	if err != nil || state.PID != os.Getpid() || len(state.Components) != 0 {
		t.Fatalf("initializing runner exposed earlier running components: %+v %v", state, err)
	}
	if active, err := RuntimeActive(dir); !active || err != nil {
		t.Fatalf("initializing runner lost ownership: %v %v", active, err)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestRunAttachesExistingHubAndKeepsKernelLock(t *testing.T) {
	dir := t.TempDir()
	ready, stopped := filepath.Join(dir, "external-ready"), filepath.Join(dir, "external-stopped")
	external := exec.Command(os.Args[0], "startup-test-client", "run", ready, stopped)
	if err := external.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = external.Process.Kill(); _ = external.Wait() }()
	waitHelperFile(t, ready)
	probe := func(context.Context) error {
		if _, err := os.Stat(ready); err == nil {
			return nil
		}
		return ErrHubUnavailable
	}
	plan := testPlan()
	plan.Binary = filepath.Join(dir, "binary-that-must-never-run")
	cancel, done := startTestRunner(t, dir, plan, RunOptions{Probe: probe})
	waitRuntimeState(t, dir, done, func(s RuntimeState) bool { return hasComponent(s, "hub", "hub_ready", 0) })
	if active, err := RuntimeActive(dir); !active || err != nil {
		t.Fatalf("running foreground owner was inactive: %v %v", active, err)
	}
	if err := Run(context.Background(), dir, plan, RunOptions{Probe: probe}); !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("duplicate runner was allowed: %v", err)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(ready); err != nil {
		t.Fatalf("attached process was stopped: %v", err)
	}
	if _, err := os.Stat(stopped); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("attached process received a shutdown signal: %v", err)
	}
	if active, err := RuntimeActive(dir); active || err != nil {
		t.Fatalf("stopped runner was active from its old snapshot: %v %v", active, err)
	}
	lock, err := acquireRuntimeLock(dir)
	if err != nil {
		t.Fatalf("kernel lock was not released: %v", err)
	}
	_ = lock.Close()
	if _, err := os.Stat(filepath.Join(dir, runtimeLockName)); err != nil {
		t.Fatalf("permanent lock file was removed: %v", err)
	}
}

func TestRunFailsClosedOnAuthenticationError(t *testing.T) {
	dir := t.TempDir()
	err := runWithPolicy(context.Background(), dir, testPlan(), RunOptions{Probe: func(context.Context) error {
		return errors.New("fake bearer token runner-fake-secret rejected")
	}}, testRunPolicy())
	if err == nil {
		t.Fatal("authentication failure was accepted")
	}
	state, readErr := ReadRuntime(dir)
	if readErr != nil || !hasComponent(state, "hub", "needs_user", 0) {
		t.Fatalf("incorrect authentication state: %+v %v", state, readErr)
	}
	if _, err := os.Stat(filepath.Join(dir, "helper-hub-count")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("Hub started after authentication failure")
	}
	logBytes, _ := os.ReadFile(filepath.Join(dir, "logs", "autostart", "runner.log"))
	if strings.Contains(string(logBytes), "runner-fake-secret") {
		t.Fatal("raw authentication error was logged")
	}
}

func TestRunStopsOwnedProcessesAndStripsHostSecrets(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("OPENAI_API_KEY", "runner-fake-secret")
	t.Setenv("ANTHROPIC_API_KEY", "runner-fake-secret")
	t.Setenv("HTTP_PROXY", "http://runner-fake-secret.invalid")
	clientReady, clientStopped := filepath.Join(dir, "client-ready"), filepath.Join(dir, "client-stopped")
	plan := testPlan()
	plan.Clients = []Client{testClient(t, "standalone", "run", clientReady, clientStopped)}
	probe := func(context.Context) error {
		if _, err := os.Stat(filepath.Join(dir, "helper-hub-ready")); err == nil {
			return nil
		}
		return ErrHubUnavailable
	}
	cancel, done := startTestRunner(t, dir, plan, RunOptions{Probe: probe})
	waitRuntimeState(t, dir, done, func(s RuntimeState) bool {
		return hasComponent(s, "client:standalone", "running", 1) && hasComponent(s, "hub", "hub_ready", 1)
	})
	waitHelperFile(t, clientReady)
	environment, err := os.ReadFile(filepath.Join(dir, "helper-hub-environment.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(environment), "runner-fake-secret") {
		t.Fatalf("host credential or proxy reached Hub: %s", environment)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	waitHelperFile(t, filepath.Join(dir, "helper-hub-stopped"))
	waitHelperFile(t, clientStopped)
	state, err := ReadRuntime(dir)
	if err != nil || !hasComponent(state, "hub", "stopped", 1) || !hasComponent(state, "client:standalone", "stopped", 1) {
		t.Fatalf("owned shutdown state: %+v %v", state, err)
	}
	for _, name := range []string{"startup-state.json", filepath.Join("logs", "autostart", "runner.log")} {
		path := filepath.Join(dir, name)
		info, err := os.Stat(path)
		// Windows maps writable POSIX mode bits to 0666; those bits do not
		// represent its ACL. Keep all file-content and lifecycle checks there.
		if err != nil || runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
			t.Fatalf("private startup file permissions: %s %v %v", path, info, err)
		}
		content, _ := os.ReadFile(path)
		if strings.Contains(string(content), "runner-fake-secret") || strings.Contains(string(content), "private-child-output") {
			t.Fatalf("child output or secrets were persisted in %s", path)
		}
	}
}

func TestRunHubFailureHasBoundedRestarts(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "helper-hub-mode"), []byte("fail"), 0600); err != nil {
		t.Fatal(err)
	}
	err := runWithPolicy(context.Background(), dir, testPlan(), RunOptions{Probe: func(context.Context) error { return ErrHubUnavailable }}, testRunPolicy())
	if err == nil {
		t.Fatal("exhausted Hub restarts returned success")
	}
	state, _ := ReadRuntime(dir)
	if !hasComponent(state, "hub", "failed", 6) {
		t.Fatalf("unexpected final Hub state: %+v", state)
	}
	count, _ := os.ReadFile(filepath.Join(dir, "helper-hub-count"))
	if got := strings.Count(string(count), "started"); got != 6 {
		t.Fatalf("Hub started %d times, want initial + 5 retries", got)
	}
}

func TestRunClientGatesAndDisabledClients(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "client-must-not-run")
	plan := testPlan()
	host := Client{Endpoint: "host", Enabled: true, RequiresHost: true}
	denied := testClient(t, "denied", "fail", marker)
	disabled := testClient(t, "disabled", "fail", marker)
	disabled.Enabled = false
	plan.Clients = []Client{host, denied, disabled}
	var gates atomic.Int32
	_, done := startTestRunner(t, dir, plan, RunOptions{
		Probe:      func(context.Context) error { return nil },
		ClientGate: func(Client) error { gates.Add(1); return errors.New("needs approval runner-fake-secret") },
	})
	waitRuntimeState(t, dir, done, func(s RuntimeState) bool {
		return hasComponent(s, "client:host", "needs_host_environment", 0) && hasComponent(s, "client:denied", "needs_user", 0)
	})
	if gates.Load() != 1 {
		t.Fatalf("gates called for disabled or host Client: %d", gates.Load())
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("gated Client was executed")
	}
}

func TestRunConcurrentRuntimeReadersDoNotInterruptTransitions(t *testing.T) {
	dir := t.TempDir()
	plan := testPlan()
	for i := 0; i < 32; i++ {
		plan.Clients = append(plan.Clients, Client{Endpoint: fmt.Sprintf("host-%02d", i), Enabled: true, RequiresHost: true})
	}
	readerCtx, stopReaders := context.WithCancel(context.Background())
	var readers sync.WaitGroup
	readErrors := make(chan error, 4)
	for i := 0; i < 4; i++ {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for readerCtx.Err() == nil {
				if _, err := ReadRuntime(dir); err != nil {
					readErrors <- err
					return
				}
				time.Sleep(time.Millisecond)
			}
		}()
	}
	t.Cleanup(func() { stopReaders(); readers.Wait() })
	cancel, done := startTestRunner(t, dir, plan, RunOptions{Probe: func(context.Context) error { return nil }})
	waitRuntimeState(t, dir, done, func(state RuntimeState) bool {
		return len(state.Components) == 33 && hasComponent(state, "client:host-31", "needs_host_environment", 0)
	})
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("concurrent UI-style runtime readers interrupted the runner: %v", err)
	}
	stopReaders()
	readers.Wait()
	select {
	case err := <-readErrors:
		t.Fatalf("concurrent runtime reader failed: %v", err)
	default:
	}
}

func TestRunClientFailuresRetryBoundedlyAndCheckGateAgain(t *testing.T) {
	for _, denyRetry := range []bool{false, true} {
		t.Run(fmt.Sprint(denyRetry), func(t *testing.T) {
			dir := t.TempDir()
			countPath := filepath.Join(dir, "client-count")
			plan := testPlan()
			plan.Clients = []Client{testClient(t, "retry", "fail", countPath)}
			var gates atomic.Int32
			_, done := startTestRunner(t, dir, plan, RunOptions{
				Probe: func(context.Context) error { return nil },
				ClientGate: func(Client) error {
					if gates.Add(1) > 1 && denyRetry {
						return errors.New("authorization changed")
					}
					return nil
				},
			})
			wantAttempts, wantState := 6, "failed"
			if denyRetry {
				wantAttempts, wantState = 1, "needs_user"
			}
			waitRuntimeState(t, dir, done, func(s RuntimeState) bool { return hasComponent(s, "client:retry", wantState, wantAttempts) })
			count, _ := os.ReadFile(countPath)
			if got := strings.Count(string(count), "started"); got != wantAttempts {
				t.Fatalf("Client attempts %d, want %d", got, wantAttempts)
			}
			if want := int32(6); !denyRetry && gates.Load() != want {
				t.Fatalf("Client prerequisites were not checked for each retry: %d", gates.Load())
			}
		})
	}
}

func TestRunSuccessfulClientIsNotRestarted(t *testing.T) {
	dir := t.TempDir()
	countPath := filepath.Join(dir, "client-count")
	plan := testPlan()
	plan.Clients = []Client{testClient(t, "once", "success", countPath)}
	_, done := startTestRunner(t, dir, plan, RunOptions{Probe: func(context.Context) error { return nil }})
	waitRuntimeState(t, dir, done, func(s RuntimeState) bool { return hasComponent(s, "client:once", "stopped", 1) })
	time.Sleep(50 * time.Millisecond)
	count, _ := os.ReadFile(countPath)
	if strings.Count(string(count), "started") != 1 {
		t.Fatal("successful Client restarted")
	}
}

func TestRuntimeEnvironmentUsesMinimalWhitelist(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "runner-fake-secret")
	t.Setenv("CLAUDE_CODE_ENTRYPOINT", "runner-fake-secret")
	t.Setenv("NODE_OPTIONS", "runner-fake-secret")
	t.Setenv("HTTP_PROXY", "http://runner-fake-secret.invalid")
	environment := strings.Join(runtimeEnvironment(map[string]string{"HTTPS_PROXY": "http://explicit.invalid", "LANG": "C"}), "\n")
	if strings.Contains(environment, "runner-fake-secret") {
		t.Fatal("ambient assistant settings crossed startup boundary")
	}
	if !strings.Contains(environment, "HTTPS_PROXY=http://explicit.invalid") || !strings.Contains(environment, "LANG=C") {
		t.Fatalf("explicit reviewed environment absent: %s", environment)
	}
}
