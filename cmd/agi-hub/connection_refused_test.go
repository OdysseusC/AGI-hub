package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"runtime"
	"strings"
	"syscall"
	"testing"

	"agihub/internal/app"
)

func TestOfflineSkillSourceAllowsOnlyActualRefusedLoopback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	address := strings.TrimPrefix(server.URL, "http://")
	server.Close()
	if err := checkSkillSource(app.Config{Listen: address, AdminToken: "local-test-only"}); err != nil {
		t.Fatal("an absent loopback Hub must permit registry-guarded offline installation:", err)
	}
}

func TestConnectionRefusalRecognizesOnlyExactPlatformErrors(t *testing.T) {
	winsock := &url.Error{Op: "Get", URL: "http://127.0.0.1:7331/v1/skill-release", Err: &net.OpError{Op: "dial", Net: "tcp", Err: os.NewSyscallError("connectex", syscall.Errno(10061))}}
	if got := connectionRefused(winsock); got != (runtime.GOOS == "windows") {
		t.Fatalf("Windows refusal recognized on %s: %v", runtime.GOOS, got)
	}
	for _, err := range []error{nil, context.DeadlineExceeded, syscall.Errno(10060), syscall.Errno(10013), fmt.Errorf("wrapped deadline: %w", context.DeadlineExceeded)} {
		if connectionRefused(err) {
			t.Fatalf("non-refusal was treated as offline: %v", err)
		}
	}
}

func TestOfflineSkillSourceStillRefusesAuthenticationFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "unauthorized", http.StatusUnauthorized) }))
	defer server.Close()
	if err := checkSkillSource(app.Config{Listen: strings.TrimPrefix(server.URL, "http://"), AdminToken: "local-test-only"}); err == nil {
		t.Fatal("authentication failure must not permit offline installation")
	}
}
