package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"agihub/internal/app"
	"agihub/internal/manager"
)

func TestClientCredentialsAreScopedPrivateAndNeverOverwrite(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg := app.Config{Listen: "127.0.0.1:7331", AdminToken: strings.Repeat("synthetic-admin-", 4), Bindings: []app.Binding{{Endpoint: manager.Endpoint{ID: "worker"}}}}
	path := filepath.Join(dir, "worker", "client.json")
	if _, err = writeClientConfig(cfg, "missing", path); err == nil {
		t.Fatal("exported an unregistered endpoint")
	}
	if _, err = writeClientConfig(cfg, "worker", path); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), cfg.AdminToken) {
		t.Fatal("export included management credential")
	}
	var payload map[string]any
	if err = json.Unmarshal(data, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["endpoint_token"] != app.EndpointToken(cfg.AdminToken, "worker") || payload["endpoint"] != "worker" || len(payload) != 4 {
		t.Fatal("incorrect scoped config")
	}
	if native, ok := payload["native"].(map[string]any); !ok || len(native) != 0 {
		t.Fatal("export invented native configuration")
	}
	if _, err = writeClientConfig(cfg, "worker", path); err == nil {
		t.Fatal("overwrote existing credentials")
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatal("credential file not private")
		}
	}
}

func TestClientCredentialsRejectRepositoryAndSymlinkParents(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg := app.Config{Bindings: []app.Binding{{Endpoint: manager.Endpoint{ID: "worker"}}}}
	repo := filepath.Join(dir, "repo")
	if err = os.MkdirAll(filepath.Join(repo, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err = writeClientConfig(cfg, "worker", filepath.Join(repo, "private", "client.json")); err == nil {
		t.Fatal("exported credentials inside repository")
	}
	link := filepath.Join(dir, "linked")
	if err = os.Symlink(dir, link); err != nil {
		t.Skip("symlinks unavailable on this platform")
	}
	if _, err = writeClientConfig(cfg, "worker", filepath.Join(link, "client.json")); err == nil {
		t.Fatal("export followed symlink ancestor")
	}
}
