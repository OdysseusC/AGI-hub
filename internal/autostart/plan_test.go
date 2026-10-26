package autostart

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func planClientForTest(t *testing.T, id string) Client {
	t.Helper()
	return Client{Endpoint: id, Command: os.Args[0], Cwd: t.TempDir()}
}

func readPlanBytes(t *testing.T, dir string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "startup.json"))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func requireNoStagedPlans(t *testing.T, dir string) {
	t.Helper()
	entries, err := filepath.Glob(filepath.Join(dir, ".startup-*"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("staged startup files remain: %v, %v", entries, err)
	}
}

func TestPlanDefaultLoadHasNoSideEffects(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "missing")
	p, err := Load(dir)
	if err != nil || p.Version != 1 || p.Enabled || p.Binary != "" || len(p.Clients) != 0 {
		t.Fatalf("default plan = %+v, %v", p, err)
	}
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("reading a default plan created its directory: %v", err)
	}
	if _, err := Load("relative"); err == nil {
		t.Fatal("accepted a relative data directory")
	}
}

func TestPlanRegistrationOnlyRecordsAndPreservesExplicitSelection(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "must-not-run")
	c := planClientForTest(t, "external-client")
	c.Enabled = true
	c.Args = []string{"startup-test-client", "success", marker}
	c.Environment = map[string]string{"LANG": "zh_CN.UTF-8", "HTTPS_PROXY": "http://127.0.0.1:8080"}
	if err := Register(dir, c); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("register executed a Client: %v", err)
	}
	p, err := Load(dir)
	if err != nil || p.Enabled || len(p.Clients) != 1 || p.Clients[0].Enabled {
		t.Fatalf("new registrations must default off: %+v, %v", p, err)
	}
	p.Clients[0].Enabled = true
	if err := Save(dir, p); err != nil {
		t.Fatal(err)
	}
	c.Args = []string{"updated", "argument with spaces", "Unicode 测试"}
	if err := Register(dir, c); err != nil {
		t.Fatal(err)
	}
	p, err = Load(dir)
	if err != nil || len(p.Clients) != 1 || !p.Clients[0].Enabled || !reflect.DeepEqual(p.Clients[0].Args, c.Args) {
		t.Fatalf("updating registration lost its explicit selection: %+v, %v", p, err)
	}
	if runtime.GOOS != "windows" {
		for _, name := range []string{"startup.json", planLockName} {
			info, err := os.Stat(filepath.Join(dir, name))
			if err != nil || info.Mode().Perm() != 0600 {
				t.Fatalf("%s is not private: %v, %v", name, info, err)
			}
		}
	}
	if err := Remove(dir, c.Endpoint); err != nil {
		t.Fatal(err)
	}
	p, err = Load(dir)
	if err != nil || len(p.Clients) != 0 {
		t.Fatalf("remove did not update plan: %+v, %v", p, err)
	}
	if err := Remove(dir, c.Endpoint); err == nil {
		t.Fatal("removing an unknown Client succeeded")
	}
	if _, err := os.Stat(filepath.Join(dir, planLockName)); err != nil {
		t.Fatalf("permanent configuration lock was removed: %v", err)
	}
	requireNoStagedPlans(t, dir)
}

func TestPlanHostOnlyCannotPersistCopiedIPCOrCredentials(t *testing.T) {
	dir := t.TempDir()
	if err := Register(dir, Client{Endpoint: "desktop", RequiresHost: true}); err != nil {
		t.Fatal(err)
	}
	before := readPlanBytes(t, dir)
	for _, mutate := range []func(*Client){
		func(c *Client) { c.Command = os.Args[0] },
		func(c *Client) { c.Cwd = dir },
		func(c *Client) { c.Args = []string{"--token", "copied-private-token"} },
		func(c *Client) { c.Environment = map[string]string{"CODEX_APP_SERVER_SOCKET": "/private/native.sock"} },
		func(c *Client) { c.Environment = map[string]string{"ANTHROPIC_API_KEY": "copied-private-token"} },
	} {
		c := Client{Endpoint: "desktop", RequiresHost: true}
		mutate(&c)
		if err := Register(dir, c); err == nil {
			t.Fatalf("accepted copied host state: %+v", c)
		}
		if after := readPlanBytes(t, dir); !bytes.Equal(before, after) || bytes.Contains(after, []byte("copied-private-token")) || bytes.Contains(after, []byte("native.sock")) {
			t.Fatal("a rejected host-only registration changed the saved plan")
		}
	}
}

func TestPlanValidationRejectsUnsafeInput(t *testing.T) {
	base := Plan{Version: 1, Clients: []Client{planClientForTest(t, "client")}}
	cases := map[string]func(*Plan){
		"unknown schema":     func(p *Plan) { p.Version = 2 },
		"enabled no binary":  func(p *Plan) { p.Enabled = true },
		"relative binary":    func(p *Plan) { p.Binary = "./agihub" },
		"binary NUL":         func(p *Plan) { p.Binary = os.Args[0] + "\x00" },
		"relative command":   func(p *Plan) { p.Clients[0].Command = "client" },
		"relative cwd":       func(p *Plan) { p.Clients[0].Cwd = "." },
		"duplicate endpoint": func(p *Plan) { p.Clients = append(p.Clients, p.Clients[0]) },
		"invalid endpoint":   func(p *Plan) { p.Clients[0].Endpoint = "../other" },
		"NUL argument":       func(p *Plan) { p.Clients[0].Args = []string{"a\x00b"} },
		"invalid UTF8":       func(p *Plan) { p.Clients[0].Args = []string{string([]byte{0xff})} },
		"too many arguments": func(p *Plan) { p.Clients[0].Args = make([]string, 129) },
		"credential flag":    func(p *Plan) { p.Clients[0].Args = []string{"--api-key=private-value"} },
		"credential next arg": func(p *Plan) {
			p.Clients[0].Args = []string{"--password", "private-value"}
		},
		"host IPC flag": func(p *Plan) { p.Clients[0].Args = []string{"--ipc-socket=/native.sock"} },
		"URL credentials": func(p *Plan) {
			p.Clients[0].Args = []string{"https://user:password@example.invalid"}
		},
		"URL credential argument": func(p *Plan) {
			p.Clients[0].Args = []string{"--proxy=http://user:password@example.invalid"}
		},
		"API environment": func(p *Plan) {
			p.Clients[0].Environment = map[string]string{"OPENAI_API_KEY": "private-value"}
		},
		"host environment": func(p *Plan) {
			p.Clients[0].Environment = map[string]string{"CODEX_APP_SERVER_SOCKET": "/native.sock"}
		},
		"loader environment": func(p *Plan) {
			p.Clients[0].Environment = map[string]string{"LD_PRELOAD": "/unreviewed.so"}
		},
		"ambiguous environment": func(p *Plan) {
			p.Clients[0].Environment = map[string]string{"SYSTEMROOT": "one", "SystemRoot": "two"}
		},
		"proxy credentials": func(p *Plan) {
			p.Clients[0].Environment = map[string]string{"HTTPS_PROXY": "http://user:password@127.0.0.1:8080"}
		},
		"proxy query": func(p *Plan) {
			p.Clients[0].Environment = map[string]string{"ALL_PROXY": "socks5://127.0.0.1:8080?token=private"}
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			p := base
			p.Clients = append([]Client(nil), base.Clients...)
			mutate(&p)
			if err := Validate(p); err == nil {
				t.Fatalf("accepted invalid plan: %+v", p)
			}
			dir := filepath.Join(t.TempDir(), "must-not-create")
			if err := Save(dir, p); err == nil {
				t.Fatal("saved invalid plan")
			}
			if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("invalid save created its data directory: %v", err)
			}
		})
	}
}

func TestPlanConfigureStagesBeforeNativeAndCommitsSelections(t *testing.T) {
	dir := t.TempDir()
	c := planClientForTest(t, "standalone")
	if err := Register(dir, c); err != nil {
		t.Fatal(err)
	}
	if err := Register(dir, Client{Endpoint: "desktop", RequiresHost: true}); err != nil {
		t.Fatal(err)
	}
	before := readPlanBytes(t, dir)
	var enabled, disabled []Registration
	ops := planPlatformOperations{
		enable: func(r Registration) error {
			if !bytes.Equal(readPlanBytes(t, dir), before) {
				t.Fatal("plan became visible before the native registration succeeded")
			}
			staged, _ := filepath.Glob(filepath.Join(dir, ".startup-*"))
			if len(staged) != 1 {
				t.Fatalf("native registration ran without a staged plan: %v", staged)
			}
			enabled = append(enabled, r)
			return nil
		},
		disable: func(r Registration) error { disabled = append(disabled, r); return nil },
	}
	if err := configureWithPlatform(dir, os.Args[0], true, []string{"standalone", "desktop"}, ops); err != nil {
		t.Fatal(err)
	}
	p, err := Load(dir)
	if err != nil || !p.Enabled || p.Binary != os.Args[0] || !p.Clients[0].Enabled || !p.Clients[1].Enabled || len(enabled) != 1 || len(disabled) != 0 {
		t.Fatalf("configured plan = %+v, native calls=%v/%v, %v", p, enabled, disabled, err)
	}
	if enabled[0] != RegistrationFor(dir, os.Args[0]) {
		t.Fatalf("wrong native registration: %+v", enabled[0])
	}
	if err := configureWithPlatform(dir, "", false, []string{}, ops); err != nil {
		t.Fatal(err)
	}
	p, err = Load(dir)
	if err != nil || p.Enabled || p.Clients[0].Enabled || p.Clients[1].Enabled || len(enabled) != 1 || len(disabled) != 1 {
		t.Fatalf("disabled plan = %+v, native calls=%v/%v, %v", p, enabled, disabled, err)
	}
	requireNoStagedPlans(t, dir)
}

func TestPlanConfigureEmptyDefaultBinaryCanDisable(t *testing.T) {
	dir := t.TempDir()
	called := 0
	ops := planPlatformOperations{
		enable: func(Registration) error { t.Fatal("disable tried to register startup"); return nil },
		disable: func(r Registration) error {
			called++
			if r.Binary != "" {
				t.Fatalf("default disable unexpectedly chose a binary: %+v", r)
			}
			return nil
		},
	}
	if err := configureWithPlatform(dir, "", false, nil, ops); err != nil {
		t.Fatal(err)
	}
	p, err := Load(dir)
	if err != nil || p.Enabled || p.Binary != "" || called != 1 {
		t.Fatalf("default disable = %+v, native calls=%d, %v", p, called, err)
	}
}

func TestPlanConfigureValidationAndNativeFailurePreserveSavedBytes(t *testing.T) {
	dir := t.TempDir()
	if err := Register(dir, planClientForTest(t, "client")); err != nil {
		t.Fatal(err)
	}
	before := readPlanBytes(t, dir)
	called := 0
	failure := errors.New("native registration failed")
	ops := planPlatformOperations{
		enable:  func(Registration) error { called++; return failure },
		disable: func(Registration) error { called++; return failure },
	}
	for _, selected := range [][]string{{"unknown"}, {"client", "client"}} {
		if err := configureWithPlatform(dir, os.Args[0], true, selected, ops); err == nil {
			t.Fatalf("accepted invalid Client selection %v", selected)
		}
	}
	if err := configureWithPlatform(dir, "relative", true, nil, ops); err == nil {
		t.Fatal("accepted a relative Hub binary")
	}
	if called != 0 {
		t.Fatal("invalid configuration reached native registration")
	}
	if err := configureWithPlatform(dir, os.Args[0], true, []string{"client"}, ops); !errors.Is(err, failure) {
		t.Fatalf("native failure = %v", err)
	}
	if !bytes.Equal(readPlanBytes(t, dir), before) {
		t.Fatal("failed configuration changed the original bytes")
	}
	requireNoStagedPlans(t, dir)
}

func TestPlanConfigureCommitFailureRestoresPreviousNativeSetting(t *testing.T) {
	for _, wasEnabled := range []bool{false, true} {
		t.Run(fmt.Sprint(wasEnabled), func(t *testing.T) {
			dir := t.TempDir()
			p := Plan{Version: 1, Enabled: wasEnabled, Binary: os.Args[0], Clients: []Client{planClientForTest(t, "client")}}
			p.Clients[0].Enabled = true
			if err := Save(dir, p); err != nil {
				t.Fatal(err)
			}
			before := readPlanBytes(t, dir)
			var calls []string
			first := true
			damageStage := func() {
				if !first {
					return
				}
				first = false
				staged, _ := filepath.Glob(filepath.Join(dir, ".startup-*"))
				if len(staged) != 1 {
					t.Fatalf("expected one staged file: %v", staged)
				}
				if err := os.Remove(staged[0]); err != nil {
					t.Fatal(err)
				}
			}
			ops := planPlatformOperations{
				enable: func(r Registration) error {
					calls = append(calls, "enable:"+r.Binary)
					damageStage()
					return nil
				},
				disable: func(r Registration) error {
					calls = append(calls, "disable:"+r.Binary)
					damageStage()
					return nil
				},
			}
			newBinary := filepath.Join(dir, "new installed agihub")
			if err := configureWithPlatform(dir, newBinary, !wasEnabled, []string{}, ops); err == nil {
				t.Fatal("simulated commit failure succeeded")
			}
			want := []string{"enable:" + newBinary, "disable:" + newBinary}
			if wasEnabled {
				want = []string{"disable:" + newBinary, "enable:" + os.Args[0]}
			}
			if !reflect.DeepEqual(calls, want) {
				t.Fatalf("rollback calls = %v, want %v", calls, want)
			}
			if !bytes.Equal(readPlanBytes(t, dir), before) {
				t.Fatal("commit failure changed the old plan")
			}
			after, err := Load(dir)
			if err != nil || !reflect.DeepEqual(after, p) {
				t.Fatalf("rollback changed Client selections: %+v, %v", after, err)
			}
			requireNoStagedPlans(t, dir)
		})
	}
}

func TestPlanRejectsSymlinkedConfigAndLock(t *testing.T) {
	for _, name := range []string{"startup.json", planLockName} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			target := filepath.Join(t.TempDir(), "private.json")
			original := []byte(`{"version":1,"enabled":false,"clients":[]}`)
			if err := os.WriteFile(target, original, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, filepath.Join(dir, name)); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			if name == "startup.json" {
				if _, err := Load(dir); err == nil {
					t.Fatal("read a symlinked startup file")
				}
			}
			if err := Save(dir, Plan{Version: 1, Clients: []Client{}}); err == nil {
				t.Fatal("saved through a symlink")
			}
			if err := Register(dir, Client{Endpoint: "host", RequiresHost: true}); err == nil {
				t.Fatal("registered through a symlink")
			}
			got, err := os.ReadFile(target)
			if err != nil || !bytes.Equal(got, original) {
				t.Fatalf("symlink target changed: %q, %v", got, err)
			}
		})
	}
}

func TestPlanConfigureReportsRollbackFailureWithoutChangingSavedPlan(t *testing.T) {
	dir := t.TempDir()
	if err := Save(dir, Plan{Version: 1, Clients: []Client{}}); err != nil {
		t.Fatal(err)
	}
	before := readPlanBytes(t, dir)
	rollbackFailure := errors.New("native rollback failed")
	ops := planPlatformOperations{
		enable: func(Registration) error {
			staged, _ := filepath.Glob(filepath.Join(dir, ".startup-*"))
			if len(staged) != 1 {
				t.Fatalf("expected one staged plan: %v", staged)
			}
			return os.Remove(staged[0])
		},
		disable: func(Registration) error { return rollbackFailure },
	}
	err := configureWithPlatform(dir, os.Args[0], true, nil, ops)
	if !errors.Is(err, rollbackFailure) || !strings.Contains(err.Error(), "restore the previous") {
		t.Fatalf("rollback failure was hidden: %v", err)
	}
	if !bytes.Equal(readPlanBytes(t, dir), before) {
		t.Fatal("a failed rollback changed the previous JSON record")
	}
}

func TestPlanStagedCommitRefusesReplacedDestination(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "startup.json")
	if err := writeJSON(path, Plan{Version: 1}); err != nil {
		t.Fatal(err)
	}
	staged, err := stageJSON(path, Plan{Version: 1, Binary: os.Args[0]})
	if err != nil {
		t.Fatal(err)
	}
	defer staged.cleanup()
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	foreign := []byte("replacement owned by another writer")
	if err := os.WriteFile(path, foreign, 0600); err != nil {
		t.Fatal(err)
	}
	if err := staged.commit(); err == nil {
		t.Fatal("overwrote a replaced configuration destination")
	}
	if got, err := os.ReadFile(path); err != nil || !bytes.Equal(got, foreign) {
		t.Fatalf("replacement was not preserved: %q, %v", got, err)
	}
}

func TestPlanRejectsOversizeUnknownAndTrailingJSON(t *testing.T) {
	dir := t.TempDir()
	for _, document := range []string{
		`{"version":1,"enabled":false,"clients":[],"unknown":true}`,
		`{"version":1,"enabled":false,"clients":[]} {}`,
		strings.Repeat(" ", maxStartupJSONBytes+1),
	} {
		if err := os.WriteFile(filepath.Join(dir, "startup.json"), []byte(document), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(dir); err == nil {
			t.Fatal("accepted an oversized, unknown-field, or trailing JSON document")
		}
	}
	p := Plan{Version: 1, Clients: []Client{planClientForTest(t, "client")}}
	p.Clients[0].Args = make([]string, 100)
	for i := range p.Clients[0].Args {
		p.Clients[0].Args[i] = strings.Repeat("x", 32768)
	}
	before := readPlanBytes(t, dir)
	if err := Save(dir, p); err == nil {
		t.Fatal("saved a plan too large to load")
	}
	if !bytes.Equal(readPlanBytes(t, dir), before) {
		t.Fatal("oversized save replaced the previous file")
	}
	requireNoStagedPlans(t, dir)
}

func TestPlanConcurrentRegisterDoesNotLoseClients(t *testing.T) {
	dir := t.TempDir()
	var group sync.WaitGroup
	errorsSeen := make(chan error, 20)
	for i := 0; i < 20; i++ {
		group.Add(1)
		go func(i int) {
			defer group.Done()
			errorsSeen <- Register(dir, Client{Endpoint: fmt.Sprintf("host-%02d", i), RequiresHost: true})
		}(i)
	}
	group.Wait()
	close(errorsSeen)
	for err := range errorsSeen {
		if err != nil {
			t.Fatal(err)
		}
	}
	p, err := Load(dir)
	if err != nil || len(p.Clients) != 20 {
		t.Fatalf("concurrent registration lost entries: count=%d, %v", len(p.Clients), err)
	}
}

func TestPlanLockProcess(t *testing.T) {
	dir := os.Getenv("AGIHUB_TEST_PLAN_LOCK_DIR")
	if dir == "" {
		return
	}
	lock, err := acquireRuntimeNamedLock(dir, planLockName)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	fmt.Fprintln(os.Stdout, "configuration-lock-held")
	_, _ = io.Copy(io.Discard, os.Stdin)
}

func TestPlanMutationFailsClearlyWhileAnotherProcessOwnsLock(t *testing.T) {
	dir := t.TempDir()
	if err := Register(dir, Client{Endpoint: "host", RequiresHost: true}); err != nil {
		t.Fatal(err)
	}
	before := readPlanBytes(t, dir)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	child := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestPlanLockProcess$")
	child.Env = append(os.Environ(), "AGIHUB_TEST_PLAN_LOCK_DIR="+dir)
	var stderr bytes.Buffer
	child.Stderr = &stderr
	stdout, err := child.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdin, err := child.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stdin.Close(); _ = child.Wait() }()
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil || strings.TrimSpace(line) != "configuration-lock-held" {
		t.Fatalf("lock helper did not start: %q, %v, %s", line, err, stderr.String())
	}
	ops := planPlatformOperations{
		enable:  func(Registration) error { t.Fatal("lock contention reached native enable"); return nil },
		disable: func(Registration) error { t.Fatal("lock contention reached native disable"); return nil },
	}
	for name, operation := range map[string]func() error{
		"save":      func() error { return Save(dir, Plan{Version: 1}) },
		"register":  func() error { return Register(dir, Client{Endpoint: "other", RequiresHost: true}) },
		"remove":    func() error { return Remove(dir, "host") },
		"configure": func() error { return configureWithPlatform(dir, os.Args[0], true, nil, ops) },
	} {
		if err := operation(); !errors.Is(err, ErrAlreadyRunning) || !strings.Contains(err.Error(), "retry") {
			t.Fatalf("%s did not report configuration lock contention: %v", name, err)
		}
	}
	if !bytes.Equal(readPlanBytes(t, dir), before) {
		t.Fatal("lock contention changed the saved configuration")
	}
	if err := stdin.Close(); err != nil {
		t.Fatal(err)
	}
	if err := child.Wait(); err != nil {
		t.Fatalf("lock helper failed: %v, %s", err, stderr.String())
	}
	if err := Register(dir, Client{Endpoint: "other", RequiresHost: true}); err != nil {
		t.Fatalf("lock did not release on process exit: %v", err)
	}
}
