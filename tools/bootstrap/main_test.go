package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"agihub/internal/app"
)

func ordinaryTemp(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func fixture(t *testing.T, hub []byte) (string, packageManifest, []byte) {
	t.Helper()
	dir := ordinaryTemp(t)
	files := map[string][]byte{binaryName("agi-hub"): hub, binaryName("agi-hub-setup"): []byte("setup"), binaryName("agi-hub-endpoint"): []byte("helper"), "START-HERE.md": []byte("public guide"), "docs/INSTALL-FOR-ASSISTANT.md": []byte("public instructions")}
	manifest := packageManifest{Version: 1, Platform: runtime.GOOS + "-" + runtime.GOARCH}
	for name, body := range files {
		full := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0700); err != nil {
			t.Fatal(err)
		}
		mode := uint32(0644)
		if name == binaryName("agi-hub") || name == binaryName("agi-hub-setup") || name == binaryName("agi-hub-endpoint") {
			mode = 0755
		}
		if err := os.WriteFile(full, body, os.FileMode(mode)); err != nil {
			t.Fatal(err)
		}
		hash := sha256.Sum256(body)
		manifest.Files = append(manifest.Files, packageFile{Path: name, SHA256: hex.EncodeToString(hash[:]), Mode: mode})
	}
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "package-manifest.json"), raw, 0644); err != nil {
		t.Fatal(err)
	}
	return dir, manifest, raw
}

func TestVerifyAndCopyAreIdempotentAndDetectDamage(t *testing.T) {
	source, manifest, raw := fixture(t, []byte("binary"))
	if _, _, err := readAndVerify(source); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(ordinaryTemp(t), "runtime")
	if err := installPayload(source, target, manifest, raw); err != nil {
		t.Fatal(err)
	}
	filename := filepath.Join(target, binaryName("agi-hub"))
	before, err := os.Stat(filename)
	if err != nil {
		t.Fatal(err)
	}
	if err := installPayload(source, target, manifest, raw); err != nil {
		t.Fatal(err)
	}
	after, _ := os.Stat(filename)
	if !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("same installation rewrote existing files")
	}
	if err := os.WriteFile(filename, []byte("local edit"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := installPayload(source, target, manifest, raw); err == nil {
		t.Fatal("expected refusal of modified runtime")
	}
	content, _ := os.ReadFile(filename)
	if string(content) != "local edit" {
		t.Fatal("modified runtime was overwritten")
	}
	if err := os.WriteFile(filepath.Join(source, "START-HERE.md"), []byte("changed"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := readAndVerify(source); err == nil {
		t.Fatal("tampered source accepted")
	}
}

func TestRejectCrossPlatformTraversalPrivateAndDuplicatePaths(t *testing.T) {
	for _, value := range []string{"../secret", "/etc/passwd", "a/../../secret", "a\\secret", "C:/secret", "clients/private/info.md", "config.json", "clients/a/client.json", ".env", "a/CON.md", "a/lpt1.txt", "trailing. ", "AUX", "a//b"} {
		if safePublicPath(value) {
			t.Errorf("unsafe path accepted: %q", value)
		}
	}
	source, manifest, _ := fixture(t, []byte("hub"))
	manifest.Files = append(manifest.Files, packageFile{Path: "start-here.md", SHA256: strings.Repeat("0", 64), Mode: 0644})
	raw, _ := json.Marshal(manifest)
	os.WriteFile(filepath.Join(source, "package-manifest.json"), raw, 0644)
	if _, _, err := readAndVerify(source); err == nil {
		t.Fatal("case-insensitive duplicate accepted")
	}
}

func TestRejectSymlinksBeforeCopy(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink privilege is not assumed")
	}
	source, _, _ := fixture(t, []byte("hub"))
	filename := filepath.Join(source, "START-HERE.md")
	body, _ := os.ReadFile(filename)
	other := filepath.Join(ordinaryTemp(t), "guide")
	os.WriteFile(other, body, 0644)
	os.Remove(filename)
	if err := os.Symlink(other, filename); err != nil {
		t.Fatal(err)
	}
	if _, _, err := readAndVerify(source); err == nil {
		t.Fatal("source symlink accepted")
	}
}

func TestVerifyOnlyDoesNotInstall(t *testing.T) {
	source, _, _ := fixture(t, []byte("hub"))
	data := filepath.Join(ordinaryTemp(t), "not-created")
	var out bytes.Buffer
	if err := run([]string{"--verify-only", "--package-dir", source, "--data", data}, &out); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(data); !os.IsNotExist(err) {
		t.Fatal("verify-only created data")
	}
	if err := run([]string{"--host", "unknown", "--package-dir", source}, &out); err == nil {
		t.Fatal("unknown host accepted")
	}
}

func TestExistingScopedHelperIsPreserved(t *testing.T) {
	skills := ordinaryTemp(t)
	folder := filepath.Join(skills, "agi-hub-client")
	os.MkdirAll(folder, 0700)
	body := []byte(`{"binary_path":"/existing/report-helper","hub_binary_path":"/existing/hub","endpoint_id":"workbuddy","data_dir":"/private/data","host":"workbuddy"}`)
	os.WriteFile(filepath.Join(folder, "installation.json"), body, 0600)
	helper, err := existingReportHelper("workbuddy", skills)
	if err != nil || helper != "/existing/report-helper" {
		t.Fatalf("helper=%q err=%v", helper, err)
	}
}

func TestProvisionedHelperIsScopedImmutableAndContainsNoAdminToken(t *testing.T) {
	installDir, _, _ := fixture(t, []byte("hub"))
	data := filepath.Join(ordinaryTemp(t), "private")
	if err := app.InitConfig(data); err != nil {
		t.Fatal(err)
	}
	cfg, err := app.LoadConfig(data)
	if err != nil {
		t.Fatal(err)
	}
	client := map[string]any{"base_url": "http://" + cfg.Listen, "endpoint": "workbuddy", "endpoint_token": app.EndpointToken(cfg.AdminToken, "workbuddy"), "native": map[string]any{}}
	body, _ := json.Marshal(client)
	clientFile := filepath.Join(data, "workbuddy-client.json")
	os.WriteFile(clientFile, body, 0600)
	workspace := ordinaryTemp(t)
	helper, err := provisionHelper(installDir, data, "workbuddy", clientFile, workspace)
	if err != nil {
		t.Fatal(err)
	}
	bindingFile := filepath.Join(filepath.Dir(helper), "binding.json")
	before, err := os.ReadFile(bindingFile)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(before), cfg.AdminToken) {
		t.Fatal("helper contains administrator credential")
	}
	var binding map[string]string
	json.Unmarshal(before, &binding)
	if binding["endpoint"] != "workbuddy" || binding["workspace_root"] != workspace || binding["endpoint_token"] != client["endpoint_token"] {
		t.Fatal("incorrect scoped helper binding")
	}
	same, err := provisionHelper(installDir, data, "workbuddy", clientFile, workspace)
	if err != nil || same != helper {
		t.Fatal("same helper provisioning was not idempotent", err)
	}
	if _, err := provisionHelper(installDir, data, "codex", clientFile, workspace); err == nil {
		t.Fatal("different endpoint accepted")
	}
	client["base_url"] = "http://127.0.0.1:1"
	wrong, _ := json.Marshal(client)
	os.WriteFile(clientFile, wrong, 0600)
	if _, err := provisionHelper(installDir, data, "workbuddy", clientFile, workspace); err == nil {
		t.Fatal("Client belonging to another Hub accepted")
	}
	client["base_url"] = "http://" + cfg.Listen
	body, _ = json.Marshal(client)
	os.WriteFile(clientFile, body, 0600)
	os.WriteFile(bindingFile, []byte("edited"), 0600)
	if _, err := provisionHelper(installDir, data, "workbuddy", clientFile, workspace); err == nil {
		t.Fatal("edited binding overwritten")
	}
	if after, _ := os.ReadFile(bindingFile); string(after) != "edited" {
		t.Fatal("edited binding was replaced")
	}
}

func TestInstallAndRerunKeepPrivateConfig(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the local Hub for an offline installer integration test")
	}
	binary := filepath.Join(ordinaryTemp(t), binaryName("agi-hub"))
	cmd := exec.Command("go", "build", "-o", binary, "./cmd/agi-hub")
	cmd.Dir = filepath.Join("..", "..")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, output)
	}
	hub, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	source, _, _ := fixture(t, hub)
	data := filepath.Join(ordinaryTemp(t), "private")
	skills := filepath.Join(ordinaryTemp(t), "skills")
	// An isolated configuration avoids touching an unrelated developer Hub on
	// the default port. The installer must preserve this pre-existing config.
	if err := app.InitConfig(data); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	listener.Close()
	config := filepath.Join(data, "config.json")
	original, err := os.ReadFile(config)
	if err != nil {
		t.Fatal(err)
	}
	var isolated map[string]any
	if err := json.Unmarshal(original, &isolated); err != nil {
		t.Fatal(err)
	}
	isolated["listen"] = address
	configured, err := json.MarshalIndent(isolated, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config, configured, 0600); err != nil {
		t.Fatal(err)
	}
	args := []string{"--host", "workbuddy", "--data", data, "--skills-dir", skills, "--package-dir", source}
	var out bytes.Buffer
	if err := run(args, &out); err != nil {
		t.Fatalf("install: %v\n%s", err, out.String())
	}
	before, err := os.ReadFile(config)
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(before, &cfg); err != nil {
		t.Fatal(err)
	}
	for _, binding := range cfg["bindings"].([]any) {
		if binding.(map[string]any)["endpoint"].(map[string]any)["enabled"] == true {
			t.Fatal("installer enabled an unverified binding")
		}
	}
	out.Reset()
	if err := run(args, &out); err != nil {
		t.Fatalf("rerun: %v\n%s", err, out.String())
	}
	after, _ := os.ReadFile(config)
	if !bytes.Equal(before, after) {
		t.Fatal("rerun changed private configuration")
	}
	if strings.Contains(out.String(), cfg["admin_token"].(string)) {
		t.Fatal("installer exposed private admin token")
	}
	if _, err := os.Stat(filepath.Join(skills, "agi-hub-client", "SKILL.md")); err != nil {
		t.Fatal(err)
	}
}
