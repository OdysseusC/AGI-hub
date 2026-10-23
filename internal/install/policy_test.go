package install

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func policyConfig(t *testing.T, opts Options, selected string) {
	t.Helper()
	if err := os.MkdirAll(opts.DataDir, 0700); err != nil {
		t.Fatal(err)
	}
	body := `{"version":1,"admin_token":"private-do-not-copy","workspaces":{"private":"secret-path"},"bindings":[` +
		`{"endpoint":{"id":"` + opts.Endpoint + `","driver":"codex"},"visible_peers":["claude"],"allowed_peers":["hidden"]},` +
		`{"endpoint":{"id":"claude","driver":"claude-code","communication_language":"` + selected + `","session_id":"secret-session"}},` +
		`{"endpoint":{"id":"hidden","driver":"claude-code","communication_language":"ja"}}]}`
	if err := os.WriteFile(filepath.Join(opts.DataDir, "config.json"), []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestPolicyTracksVisibleLanguagesWithoutCopyingPrivateConfig(t *testing.T) {
	opts := testOptions(t)
	policyConfig(t, opts, "")
	folder, err := Skill(opts)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(folder, "communication-policy.md")
	initial := mustRead(t, path)
	for _, expected := range []string{`"id": "claude"`, `"communication_language": "en"`, `"send_language": "en"`, opts.Endpoint, "peers", "human"} {
		if !bytes.Contains(initial, []byte(expected)) {
			t.Fatalf("policy omitted %q: %s", expected, initial)
		}
	}
	for _, secret := range []string{"private-do-not-copy", "secret-path", "secret-session", `"id": "hidden"`} {
		if bytes.Contains(initial, []byte(secret)) {
			t.Fatalf("policy leaked non-policy information %q", secret)
		}
	}
	beforeSkill := mustRead(t, filepath.Join(folder, "SKILL.md"))
	beforeRegistry := mustRead(t, filepath.Join(opts.DataDir, registryName))
	policyConfig(t, opts, "ru")
	status, err := Status(opts.DataDir)
	if err != nil || len(status) != 1 || status[0].State != "outdated" {
		t.Fatalf("policy change was not visible to read-only status: %+v, %v", status, err)
	}
	results, err := Sync(opts.DataDir)
	if err != nil || len(results) != 1 || results[0].State != "updated" || results[0].HostLoadState != "unknown" {
		t.Fatalf("policy-only sync failed: %+v, %v", results, err)
	}
	if !bytes.Contains(mustRead(t, path), []byte(`"send_language": "ru"`)) {
		t.Fatal("updated recipient language did not reach installed skill")
	}
	if !bytes.Equal(beforeSkill, mustRead(t, filepath.Join(folder, "SKILL.md"))) || !bytes.Equal(beforeRegistry, mustRead(t, filepath.Join(opts.DataDir, registryName))) {
		t.Fatal("personal language change altered source rules or release approval")
	}
	stamp := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes(path, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	results, err = Sync(opts.DataDir)
	info, statErr := os.Stat(path)
	if err != nil || results[0].State != "current" || statErr != nil || !info.ModTime().Equal(stamp) {
		t.Fatalf("no-op policy sync rewrote file: %+v %v %v", results, err, statErr)
	}
}

func TestPolicyRespectsChineseAndProtectsLocalEdits(t *testing.T) {
	opts := testOptions(t)
	policyConfig(t, opts, "zh-Hans")
	folder, err := Skill(opts)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(folder, "communication-policy.md")
	if !bytes.Contains(mustRead(t, path), []byte(`"send_language": "zh-Hans"`)) {
		t.Fatal("explicit Chinese policy was rejected or replaced")
	}
	if err := os.WriteFile(path, []byte("local policy edit"), 0600); err != nil {
		t.Fatal(err)
	}
	policyConfig(t, opts, "fr")
	results, err := Sync(opts.DataDir)
	if err != nil || len(results) != 1 || results[0].State != "conflict" {
		t.Fatalf("edited policy was not protected: %+v, %v", results, err)
	}
	if string(mustRead(t, path)) != "local policy edit" {
		t.Fatal("local policy edit was overwritten")
	}
}

func TestPolicyUpgradePreservesUnmanagedFileAndAcceptsOldManifest(t *testing.T) {
	for _, collision := range []bool{false, true} {
		t.Run(map[bool]string{false: "upgrade", true: "unmanaged_collision"}[collision], func(t *testing.T) {
			opts := testOptions(t)
			folder, err := Skill(opts)
			if err != nil {
				t.Fatal(err)
			}
			files := map[string]string{}
			for _, name := range []string{"SKILL.md", "installation.json"} {
				files[name] = checksum(mustRead(t, filepath.Join(folder, name)))
			}
			legacy, _ := json.Marshal(manifest{Owner: owner, Version: 1, Files: files})
			if err := os.WriteFile(filepath.Join(folder, manifestName), legacy, 0600); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(folder, "communication-policy.md")
			if collision {
				if err := os.WriteFile(path, []byte("user-owned policy"), 0600); err != nil {
					t.Fatal(err)
				}
			} else if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			results, err := Sync(opts.DataDir)
			expected := "updated"
			if collision {
				expected = "conflict"
			}
			if err != nil || len(results) != 1 || results[0].State != expected {
				t.Fatalf("legacy policy migration: %+v %v", results, err)
			}
			if collision {
				if string(mustRead(t, path)) != "user-owned policy" {
					t.Fatal("unmanaged file was replaced during upgrade")
				}
			} else if _, err := verifyOwned(folder); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestMalformedPolicyConfigCannotOverwriteInstalledFiles(t *testing.T) {
	for _, body := range []string{`{`, `{}`, `null`, `{"version":1,"bindings":[]}`, `{"version":1,"bindings":[]} {}`} {
		t.Run(body, func(t *testing.T) {
			opts := testOptions(t)
			folder, err := Skill(opts)
			if err != nil {
				t.Fatal(err)
			}
			before := mustRead(t, filepath.Join(folder, "communication-policy.md"))
			if err := os.WriteFile(filepath.Join(opts.DataDir, "config.json"), []byte(body), 0600); err != nil {
				t.Fatal(err)
			}
			results, err := Sync(opts.DataDir)
			if err == nil && (len(results) != 1 || results[0].State != "error") {
				t.Fatalf("malformed config accepted: %+v, %v", results, err)
			}
			if !bytes.Equal(before, mustRead(t, filepath.Join(folder, "communication-policy.md"))) {
				t.Fatal("malformed config replaced installed policy")
			}
		})
	}
}

func TestPolicyWithoutConfigKeepsInstallationUsable(t *testing.T) {
	opts := testOptions(t)
	opts.Host = "claude"
	folder, err := Skill(opts)
	if err != nil {
		t.Fatal(err)
	}
	policy := string(mustRead(t, filepath.Join(folder, "communication-policy.md")))
	if !strings.Contains(policy, `"communication_language": "en"`) || !strings.Contains(policy, `"id": "`+opts.Endpoint+`"`) {
		t.Fatalf("standalone installation omitted its own default policy: %s", policy)
	}
}
