package main

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPackageContainsOfflineInstallerAndOnlyPublicPayload(t *testing.T) {
	dir := t.TempDir()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(previous) })
	files := map[string]string{
		"hub": "binary", "setup": "installer", "helper": "scoped helper", "START-HERE.md": "guide", "INSTALL.md": "guide", "README.md": "readme", "README.zh-CN.md": "readme", "LICENSE": "license", "config.example.json": "public example", "tools/smoke_client.py": "public code", "tools/package/install.sh": "launcher",
		"docs/INSTALL-FOR-ASSISTANT.md": "assistant guide", "docs/private/secret.md": "PRIVATE DOCUMENT", "docs/unlisted.json": "PRIVATE DATA",
		"clients/workbuddy/main.py": "public code", "clients/workbuddy/client.example.json": "public example", "clients/shared/report.py": "public code",
		"clients/workbuddy/client.json": "SECRET TOKEN", "clients/workbuddy/private/secret.md": "PRIVATE CLIENT", "clients/workbuddy/__pycache__/x.pyc": "CACHE",
		"clients/claude-code/.claude-plugin/marketplace.json": "public marketplace", "clients/claude-code/agi-hub-endpoint/hooks/hooks.json": "public hooks",
		"tools/smoke_skill_sync.py": "public skill smoke", "tools/smoke_mailbox.py": "public mailbox smoke", "tools/MAILBOX-SMOKE.md": "public smoke instructions",
	}
	for name, body := range files {
		if err := os.MkdirAll(filepath.Dir(name), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(name, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	archive := filepath.Join(dir, "bundle.zip")
	if err := packageBinary(archive, "hub", "agi-hub", "setup", "agi-hub-setup", "helper", "agi-hub-endpoint", "linux-amd64"); err != nil {
		t.Fatal(err)
	}
	r, err := zip.OpenReader(archive)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	payload := map[string][]byte{}
	for _, entry := range r.File {
		in, err := entry.Open()
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(in)
		in.Close()
		if err != nil {
			t.Fatal(err)
		}
		payload[entry.Name] = body
		if strings.Contains(string(body), "SECRET TOKEN") || strings.Contains(string(body), "PRIVATE ") {
			t.Fatalf("private payload included: %s", entry.Name)
		}
	}
	for _, name := range []string{"agi-hub", "agi-hub-setup", "agi-hub-endpoint", "START-HERE.md", "INSTALL.md", "install.sh", "clients/shared/report.py", "clients/workbuddy/client.example.json", "clients/claude-code/.claude-plugin/marketplace.json", "clients/claude-code/agi-hub-endpoint/hooks/hooks.json", "package-manifest.json"} {
		if _, ok := payload[name]; !ok {
			t.Errorf("missing %s", name)
		}
	}
	for _, name := range []string{"docs/private/secret.md", "docs/unlisted.json", "clients/workbuddy/client.json", "clients/workbuddy/private/secret.md", "clients/workbuddy/__pycache__/x.pyc"} {
		if _, ok := payload[name]; ok {
			t.Errorf("private file included: %s", name)
		}
	}
	var manifest packageManifest
	if err := json.Unmarshal(payload["package-manifest.json"], &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Platform != "linux-amd64" || manifest.Version != 1 {
		t.Fatal("incorrect package identity")
	}
	if len(manifest.Files) != len(payload)-1 {
		t.Fatal("manifest does not cover every public file")
	}
	for _, entry := range manifest.Files {
		body, ok := payload[entry.Path]
		if !ok {
			t.Fatalf("missing manifest entry %s", entry.Path)
		}
		hash := sha256.Sum256(body)
		if entry.SHA256 != hex.EncodeToString(hash[:]) {
			t.Errorf("incorrect digest for %s", entry.Path)
		}
	}
}
