//go:build darwin || linux

package autostart

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestRunStopsOnlyOwnedProcessTree(t *testing.T) {
	dir := t.TempDir()
	ready, stopped := filepath.Join(dir, "parent-ready"), filepath.Join(dir, "parent-stopped")
	childReady, childStopped := filepath.Join(dir, "child-ready"), filepath.Join(dir, "child-stopped")
	childPIDFile := filepath.Join(dir, "child-pid")
	plan := testPlan()
	plan.Clients = []Client{testClient(t, "tree", "tree", ready, stopped, childReady, childStopped, childPIDFile)}
	cancel, done := startTestRunner(t, dir, plan, RunOptions{Probe: func(context.Context) error { return nil }})
	waitHelperFile(t, ready)
	waitHelperFile(t, childReady)
	pidBytes, err := os.ReadFile(childPIDFile)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(pidBytes)))
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	waitHelperFile(t, stopped)
	waitHelperFile(t, childStopped)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && syscall.Kill(pid, 0) == nil {
		time.Sleep(5 * time.Millisecond)
	}
	if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("owned descendant still exists after shutdown: %v", err)
	}
}

func TestRuntimeLockAndLogRejectSymlinks(t *testing.T) {
	for _, name := range []string{runtimeLockName, filepath.Join("logs", "autostart", "runner.log")} {
		t.Run(filepath.Base(name), func(t *testing.T) {
			dir := t.TempDir()
			target := filepath.Join(t.TempDir(), "private-target")
			if err := os.WriteFile(target, []byte("untouched"), 0600); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, name)
			if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, path); err != nil {
				t.Fatal(err)
			}
			err := Run(context.Background(), dir, testPlan(), RunOptions{Probe: func(context.Context) error { return nil }})
			if err == nil {
				t.Fatal("startup accepted a symlink for its private lock or log")
			}
			if name == runtimeLockName {
				if active, err := RuntimeActive(dir); active || err == nil {
					t.Fatalf("invalid runtime lock was counted as active: %v %v", active, err)
				}
			}
			content, _ := os.ReadFile(target)
			if string(content) != "untouched" {
				t.Fatalf("startup modified symlink destination: %q", content)
			}
		})
	}
}
