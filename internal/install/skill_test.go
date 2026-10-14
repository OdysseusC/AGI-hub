package install

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

func testOptions(t *testing.T) Options {
	t.Helper()
	// macOS exposes its temporary directory through /var, which can itself be
	// a symlink. Use its real path because the installer rejects linked roots.
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return Options{Host: "workbuddy", Endpoint: "workbuddy-test", BinaryPath: filepath.Join(root, "agi-hub"), DataDir: filepath.Join(root, "private"), SkillsDir: filepath.Join(root, "skills")}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestSkillInstallationIsIdempotent(t *testing.T) {
	opts := testOptions(t)
	folder, err := Skill(opts)
	if err != nil {
		t.Fatal(err)
	}
	if folder != filepath.Join(opts.SkillsDir, skillName) {
		t.Fatalf("unexpected installed folder: %s", folder)
	}
	stamp := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, name := range []string{"SKILL.md", "installation.json", manifestName} {
		if err := os.Chtimes(filepath.Join(folder, name), stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}
	again, err := Skill(opts)
	if err != nil || again != folder {
		t.Fatalf("repeat install: %s, %v", again, err)
	}
	for _, name := range []string{"SKILL.md", "installation.json", manifestName} {
		info, err := os.Stat(filepath.Join(folder, name))
		if err != nil || !info.ModTime().Equal(stamp) {
			t.Fatalf("idempotent install rewrote %s: %v", name, err)
		}
	}
	if _, err := verifyOwned(folder); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(opts.SkillsDir, "."+skillName+"-install.lock")); !os.IsNotExist(err) {
		t.Fatalf("lock was left after successful install: %v", err)
	}
}

func TestOwnedUneditedSkillCanUpgradeAndPreservesOtherFiles(t *testing.T) {
	opts := testOptions(t)
	folder, err := Skill(opts)
	if err != nil {
		t.Fatal(err)
	}
	localNote := filepath.Join(folder, "my-notes.txt")
	if err := os.WriteFile(localNote, []byte("user-owned note\n"), 0600); err != nil {
		t.Fatal(err)
	}
	original := mustRead(t, filepath.Join(folder, "SKILL.md"))
	newContent := append(append([]byte{}, original...), []byte("\nVerified upgrade.\n")...)
	opts.Endpoint = "workbuddy-updated"
	updated, err := installSkill(opts, func() ([]byte, error) { return newContent, nil })
	if err != nil || updated != folder {
		t.Fatalf("upgrade failed: %s, %v", updated, err)
	}
	if !bytes.Equal(mustRead(t, filepath.Join(folder, "SKILL.md")), newContent) {
		t.Fatal("owned skill content was not updated")
	}
	if string(mustRead(t, localNote)) != "user-owned note\n" {
		t.Fatal("installer changed an unmanaged local note")
	}
	var metadata installation
	if err := json.Unmarshal(mustRead(t, filepath.Join(folder, "installation.json")), &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata.EndpointID != opts.Endpoint {
		t.Fatal("metadata was not upgraded")
	}
	if _, err := verifyOwned(folder); err != nil {
		t.Fatalf("new manifest did not match upgraded files: %v", err)
	}
}

func TestRefusesUnmanagedAndEditedFiles(t *testing.T) {
	t.Run("unmanaged", func(t *testing.T) {
		opts := testOptions(t)
		folder := filepath.Join(opts.SkillsDir, skillName)
		if err := os.MkdirAll(folder, 0700); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(folder, "SKILL.md")
		if err := os.WriteFile(path, []byte("belongs to someone else"), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := Skill(opts); err == nil {
			t.Fatal("overwrote an unmanaged skill")
		}
		if string(mustRead(t, path)) != "belongs to someone else" {
			t.Fatal("unmanaged content was changed")
		}
	})
	for _, name := range []string{"SKILL.md", "installation.json"} {
		t.Run("edited_"+name, func(t *testing.T) {
			opts := testOptions(t)
			folder, err := Skill(opts)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(folder, name)
			if err := os.WriteFile(path, []byte("local edit"), 0600); err != nil {
				t.Fatal(err)
			}
			opts.Endpoint = "new-endpoint"
			if _, err := Skill(opts); err == nil {
				t.Fatalf("overwrote locally edited %s", name)
			}
			if string(mustRead(t, path)) != "local edit" {
				t.Fatal("local edit was lost")
			}
		})
	}
}

func TestInstallationMetadataContainsOnlyPublicRoutingFields(t *testing.T) {
	opts := testOptions(t)
	folder, err := Skill(opts)
	if err != nil {
		t.Fatal(err)
	}
	var metadata map[string]string
	if err := json.Unmarshal(mustRead(t, filepath.Join(folder, "installation.json")), &metadata); err != nil {
		t.Fatal(err)
	}
	keys := make([]string, 0, len(metadata))
	for key := range metadata {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	if !reflect.DeepEqual(keys, []string{"binary_path", "data_dir", "endpoint_id", "host"}) {
		t.Fatalf("metadata includes unexpected fields: %v", keys)
	}
	if metadata["host"] != opts.Host || metadata["endpoint_id"] != opts.Endpoint || metadata["binary_path"] != opts.BinaryPath || metadata["data_dir"] != opts.DataDir {
		t.Fatalf("metadata lost exact installation values: %v", metadata)
	}
}

func TestHostDefaultsAndExplicitOtherHostDirectory(t *testing.T) {
	opts := testOptions(t)
	opts.SkillsDir = ""
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	for host, subdir := range map[string]string{"codex": ".codex", "workbuddy": ".workbuddy"} {
		opts.Host = host
		resolved, err := normalizeOptions(opts)
		if err != nil || resolved.SkillsDir != filepath.Join(home, subdir, "skills") {
			t.Fatalf("default for %s: %+v, %v", host, resolved, err)
		}
	}
	opts.Host = "claude"
	if _, err := normalizeOptions(opts); err == nil {
		t.Fatal("invented an implicit skills directory for an unverified host")
	}
	opts.SkillsDir = testOptions(t).SkillsDir
	if _, err := normalizeOptions(opts); err != nil {
		t.Fatalf("explicit directory for another host rejected: %v", err)
	}
}

func TestRefusesInvalidPathsAndIdentifiers(t *testing.T) {
	tests := []struct {
		name string
		edit func(*Options)
	}{
		{"relative skills", func(o *Options) { o.SkillsDir = "skills" }},
		{"skills traversal", func(o *Options) {
			o.SkillsDir += string(filepath.Separator) + ".." + string(filepath.Separator) + "elsewhere"
		}},
		{"relative binary", func(o *Options) { o.BinaryPath = "agi-hub" }},
		{"relative data", func(o *Options) { o.DataDir = "private" }},
		{"empty endpoint", func(o *Options) { o.Endpoint = "" }},
		{"endpoint control", func(o *Options) { o.Endpoint = "workbuddy\nother" }},
		{"host traversal", func(o *Options) { o.Host = "../host" }},
		{"root skills", func(o *Options) { o.SkillsDir = filepath.VolumeName(o.SkillsDir) + string(filepath.Separator) }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			opts := testOptions(t)
			test.edit(&opts)
			if _, err := Skill(opts); err == nil {
				t.Fatal("invalid installation was accepted")
			}
		})
	}
}

func symlinkOrSkip(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable on this test platform/account: %v", err)
	}
}

func TestRefusesSymlinkDestinationAndParents(t *testing.T) {
	for _, kind := range []string{"skills root", "parent component", "skill folder"} {
		t.Run(kind, func(t *testing.T) {
			opts := testOptions(t)
			outside := filepath.Join(filepath.Dir(opts.SkillsDir), "outside")
			if err := os.MkdirAll(outside, 0700); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "skills root":
				symlinkOrSkip(t, outside, opts.SkillsDir)
			case "parent component":
				symlinkOrSkip(t, outside, opts.SkillsDir)
				opts.SkillsDir = filepath.Join(opts.SkillsDir, "nested")
			case "skill folder":
				if err := os.MkdirAll(opts.SkillsDir, 0700); err != nil {
					t.Fatal(err)
				}
				symlinkOrSkip(t, outside, filepath.Join(opts.SkillsDir, skillName))
			}
			if _, err := Skill(opts); err == nil {
				t.Fatal("symlink installation accepted")
			}
			entries, err := os.ReadDir(outside)
			if err != nil || len(entries) != 0 {
				t.Fatalf("wrote through destination symlink: %v, %v", entries, err)
			}
		})
	}
}

func TestRefusesSymlinkManagedFiles(t *testing.T) {
	for _, name := range []string{"SKILL.md", "installation.json", manifestName} {
		t.Run(name, func(t *testing.T) {
			opts := testOptions(t)
			folder, err := Skill(opts)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(folder, name)
			outside := filepath.Join(filepath.Dir(opts.SkillsDir), "outside.txt")
			original := mustRead(t, path)
			if err := os.WriteFile(outside, original, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			symlinkOrSkip(t, outside, path)
			opts.Endpoint = "updated"
			if _, err := Skill(opts); err == nil {
				t.Fatal("managed file symlink accepted")
			}
			if !bytes.Equal(mustRead(t, outside), original) {
				t.Fatal("outside file was modified through symlink")
			}
		})
	}
}

func TestRejectsInvalidManifestAndKeepsStaleLock(t *testing.T) {
	t.Run("manifest path escape", func(t *testing.T) {
		opts := testOptions(t)
		folder, err := Skill(opts)
		if err != nil {
			t.Fatal(err)
		}
		m := manifest{Owner: owner, Version: 1, Files: map[string]string{"SKILL.md": checksum(mustRead(t, filepath.Join(folder, "SKILL.md"))), "../installation.json": "invalid"}}
		data, err := json.Marshal(m)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(folder, manifestName), data, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := Skill(opts); err == nil {
			t.Fatal("escaping manifest accepted")
		}
	})
	t.Run("stale lock", func(t *testing.T) {
		opts := testOptions(t)
		if err := os.MkdirAll(opts.SkillsDir, 0700); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(opts.SkillsDir, "."+skillName+"-install.lock")
		if err := os.WriteFile(path, []byte("existing lock"), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := Skill(opts); err == nil || !strings.Contains(err.Error(), "lock") {
			t.Fatalf("did not refuse existing lock: %v", err)
		}
		if string(mustRead(t, path)) != "existing lock" {
			t.Fatal("existing lock was removed")
		}
	})
}
