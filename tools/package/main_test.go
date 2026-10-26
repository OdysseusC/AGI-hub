package main

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func packageFixture(t *testing.T) string {
	t.Helper()
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
		"hub": "binary", "setup": "installer", "helper": "scoped helper", "LICENSE": "license", "config.example.json": "public example", "tools/smoke_client.py": "public code", "tools/package/install.command": "macOS launcher", "tools/package/install.ps1": "Windows launcher",
		"docs/private/secret.md": "PRIVATE DOCUMENT", "docs/unlisted.json": "PRIVATE DATA",
		"clients/workbuddy/main.py": "public code", "clients/workbuddy/client.example.json": "public example", "clients/shared/report.py": "public code",
		"clients/workbuddy/client.json": "SECRET TOKEN", "clients/workbuddy/private/secret.md": "PRIVATE CLIENT", "clients/workbuddy/__pycache__/x.pyc": "CACHE",
		"clients/claude-code/.claude-plugin/marketplace.json": "public marketplace", "clients/claude-code/agi-hub-endpoint/hooks/hooks.json": "public hooks",
		"tools/smoke_skill_sync.py": "public skill smoke", "tools/smoke_mailbox.py": "public mailbox smoke", "tools/smoke_startup.py": "public startup smoke",
	}
	for _, name := range publicDocumentFixtures() {
		files[name] = "Chinese default document"
		files[strings.TrimSuffix(name, ".md")+".en.md"] = "English optional document"
	}
	for name, body := range files {
		if err := os.MkdirAll(filepath.Dir(name), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(name, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func publicDocumentFixtures() []string {
	return []string{
		"AGENTS.md", "CHANGELOG.md", "CONTEXT.md", "CONTRIBUTING.md", "INSTALL.md", "README.md", "START-HERE.md",
		"docs/ADAPTERS.md", "docs/ARCHITECTURE.md", "docs/AUTOSTART.md", "docs/CLIENTS.md", "docs/INSTALL-FOR-ASSISTANT.md", "docs/NATIVE-VALIDATION.md", "docs/PROTOCOL.md", "docs/SKILLS.md", "docs/TEAM-ORCHESTRATION.md", "docs/adr/0001-native-coordination.md",
		"clients/claude-code/README.md", "clients/codex/README.md", "clients/grok-routine/README.md", "clients/tests/README.md", "clients/workbuddy/README.md",
		"tools/MAILBOX-SMOKE.md", "internal/install/skills/agi-hub-client/SKILL.md",
	}
}

func TestPackageContainsOfflineInstallerAndOnlyPublicPayload(t *testing.T) {
	dir := packageFixture(t)
	archive := filepath.Join(dir, "bundle.zip")
	if err := packageBinary(archive, "hub", "agi-hub", "setup", "agi-hub-setup", "helper", "agi-hub-endpoint", "darwin-amd64"); err != nil {
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
	for _, name := range []string{"agi-hub", "agi-hub-setup", "agi-hub-endpoint", "START-HERE.md", "INSTALL.md", "install.command", "clients/shared/report.py", "clients/workbuddy/client.example.json", "clients/claude-code/.claude-plugin/marketplace.json", "clients/claude-code/agi-hub-endpoint/hooks/hooks.json", "package-manifest.json"} {
		if _, ok := payload[name]; !ok {
			t.Errorf("missing %s", name)
		}
	}
	for _, name := range publicDocumentFixtures() {
		if string(payload[name]) != "Chinese default document" {
			t.Errorf("missing Chinese default document %s", name)
		}
		english := strings.TrimSuffix(name, ".md") + ".en.md"
		if string(payload[english]) != "English optional document" {
			t.Errorf("missing English optional document %s", english)
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
	if manifest.Platform != "darwin-amd64" || manifest.Version != 1 {
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

func TestBuildPublishesOnlyWindowsAndMacOSNamesWithNativeManifests(t *testing.T) {
	packageFixture(t)
	if err := os.Mkdir("dist", 0755); err != nil {
		t.Fatal(err)
	}
	stale := []string{"agi-hub-darwin-amd64.zip", "agi-hub-darwin-arm64.zip", "agi-hub-linux-amd64.zip", "agi-hub-linux-arm64.zip"}
	for _, name := range append(stale, "SHA256SUMS", "unrelated.zip") {
		if err := os.WriteFile(filepath.Join("dist", name), []byte("old output"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	compiled := 0
	err := buildPackages(func(target buildTarget, binary, source string) error {
		compiled++
		if target.PublicOS == "macos" && target.GOOS != "darwin" {
			t.Error("friendly macOS name leaked into GOOS")
		}
		if target.GOOS != "darwin" && target.GOOS != "windows" {
			t.Errorf("unexpected release target: %+v", target)
		}
		if filepath.Dir(binary) != filepath.Join("dist", target.PublicOS+"-"+target.Arch) {
			t.Errorf("unexpected stage path: %s", binary)
		}
		return os.WriteFile(binary, []byte(target.GOOS+"/"+target.Arch+":"+source), 0755)
	})
	if err != nil {
		t.Fatal(err)
	}
	if compiled != 12 {
		t.Fatalf("compiled %d programs, want 4 targets x 3 programs", compiled)
	}
	content, err := os.ReadFile(filepath.Join("dist", "SHA256SUMS"))
	if err != nil {
		t.Fatal(err)
	}
	expected := map[string]string{
		"agi-hub-macos-amd64.zip": "darwin-amd64", "agi-hub-macos-arm64.zip": "darwin-arm64",
		"agi-hub-windows-amd64.zip": "windows-amd64", "agi-hub-windows-arm64.zip": "windows-arm64",
	}
	lines := strings.Split(strings.TrimSpace(string(content)), "\n")
	if len(lines) != len(expected) {
		t.Fatalf("unexpected release checksum count: %s", content)
	}
	seen := map[string]bool{}
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) != 2 || expected[fields[1]] == "" || seen[fields[1]] {
			t.Fatalf("unexpected checksum entry: %s", line)
		}
		name := fields[1]
		seen[name] = true
		body, err := os.ReadFile(filepath.Join("dist", name))
		if err != nil {
			t.Fatal(err)
		}
		hash := sha256.Sum256(body)
		if fields[0] != hex.EncodeToString(hash[:]) {
			t.Errorf("incorrect ZIP checksum: %s", name)
		}
		archive, err := zip.OpenReader(filepath.Join("dist", name))
		if err != nil {
			t.Fatal(err)
		}
		entries := map[string]*zip.File{}
		for _, file := range archive.File {
			entries[file.Name] = file
		}
		manifestFile, err := entries["package-manifest.json"].Open()
		if err != nil {
			t.Fatal(err)
		}
		var manifest packageManifest
		err = json.NewDecoder(manifestFile).Decode(&manifest)
		manifestFile.Close()
		if err != nil || manifest.Platform != expected[name] {
			t.Fatalf("native manifest changed for %s: %+v %v", name, manifest, err)
		}
		launcher, binary := "install.command", "agi-hub"
		if strings.HasPrefix(name, "agi-hub-windows-") {
			launcher, binary = "install.ps1", "agi-hub.exe"
		}
		if entries[launcher] == nil || entries[binary] == nil || entries["install.sh"] != nil {
			t.Errorf("wrong native package payload: %s", name)
		}
		archive.Close()
	}
	for _, name := range stale {
		if _, err := os.Stat(filepath.Join("dist", name)); !os.IsNotExist(err) {
			t.Errorf("stale former release archive retained: %s", name)
		}
	}
	if _, err := os.Stat(filepath.Join("dist", "unrelated.zip")); err != nil {
		t.Fatal("unrelated output removed", err)
	}
}

func TestFailedBuildDoesNotLeaveLegacyChecksums(t *testing.T) {
	packageFixture(t)
	if err := os.Mkdir("dist", 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join("dist", "SHA256SUMS"), []byte("old linux checksum"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := buildPackages(func(buildTarget, string, string) error { return errors.New("synthetic compiler failure") }); err == nil {
		t.Fatal("failed compiler did not stop packaging")
	}
	if _, err := os.Stat(filepath.Join("dist", "SHA256SUMS")); !os.IsNotExist(err) {
		t.Fatal("failed build left an old checksum list")
	}
}
