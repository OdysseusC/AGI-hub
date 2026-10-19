package install

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func sourceText(text string) func() ([]byte, error) {
	return func() ([]byte, error) { return []byte(text), nil }
}

func TestExplicitInstallRegistersOnceAndKeepsRegistryPrivate(t *testing.T) {
	opts := testOptions(t)
	if _, err := Skill(opts); err != nil {
		t.Fatal(err)
	}
	registry, err := readRegistry(opts.DataDir)
	if err != nil || len(registry.Installations) != 1 {
		t.Fatalf("explicit install was not registered: %+v, %v", registry, err)
	}
	record := registry.Installations[0]
	if record.Host != opts.Host || record.EndpointID != opts.Endpoint || record.BinaryPath != opts.BinaryPath || record.SkillsDir != opts.SkillsDir {
		t.Fatalf("registry lost explicit installation options: %+v", record)
	}
	var public map[string]any
	data := mustRead(t, filepath.Join(opts.DataDir, registryName))
	if err := json.Unmarshal(data, &public); err != nil {
		t.Fatal(err)
	}
	if len(public) != 5 || public["source_revision"] != float64(SkillRevision) {
		t.Fatalf("unexpected registry fields: %s", data)
	}
	entries := public["installations"].([]any)
	if len(entries[0].(map[string]any)) != 4 {
		t.Fatalf("registry contains fields beyond installation routing: %s", data)
	}
	stamp := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	path := filepath.Join(opts.DataDir, registryName)
	if err := os.Chtimes(path, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	if _, err := Skill(opts); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || !info.ModTime().Equal(stamp) {
		t.Fatalf("unchanged install rewrote registry: %v", err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0 {
		t.Fatalf("registry is accessible to other users: %v", info.Mode())
	}
	// An explicit rebind replaces the existing path's record, not a second
	// subscription to the same files.
	opts.Endpoint = "new-endpoint"
	if _, err := Skill(opts); err != nil {
		t.Fatal(err)
	}
	registry, err = readRegistry(opts.DataDir)
	if err != nil || len(registry.Installations) != 1 || registry.Installations[0].EndpointID != opts.Endpoint {
		t.Fatalf("explicit rebind did not replace registration: %+v, %v", registry, err)
	}
}

func TestSyncUpdatesOnlyRegisteredOwnedFilesAndDoesNotClaimHostReload(t *testing.T) {
	opts := testOptions(t)
	folder, err := Skill(opts)
	if err != nil {
		t.Fatal(err)
	}
	note := filepath.Join(folder, "my-notes.md")
	if err := os.WriteFile(note, []byte("leave my notes intact"), 0600); err != nil {
		t.Fatal(err)
	}
	unregistered := opts
	unregistered.SkillsDir = filepath.Join(filepath.Dir(opts.SkillsDir), "not-registered")
	otherFolder, err := installSkill(unregistered, sourceText("not registered"))
	if err != nil {
		t.Fatal(err)
	}
	results, err := syncSkillsAtRevision(opts.DataDir, "", sourceText("new managed rules\n"), SkillRevision+1)
	if err != nil || len(results) != 1 {
		t.Fatalf("unexpected sync results: %+v, %v", results, err)
	}
	result := results[0]
	if result.State != "updated" || result.DesiredVersion != skillVersion([]byte("new managed rules\n")) || result.DiskVersion != result.DesiredVersion || result.HostLoadState != "unknown" {
		t.Fatalf("incorrect file/update evidence: %+v", result)
	}
	if string(mustRead(t, filepath.Join(folder, "SKILL.md"))) != "new managed rules\n" || string(mustRead(t, note)) != "leave my notes intact" {
		t.Fatal("sync did not preserve managed/unmanaged boundaries")
	}
	if string(mustRead(t, filepath.Join(otherFolder, "SKILL.md"))) != "not registered" {
		t.Fatal("sync scanned or updated an unregistered installation")
	}
	results, err = syncSkillsAtRevision(opts.DataDir, "", sourceText("new managed rules\n"), SkillRevision+1)
	if err != nil || len(results) != 1 || results[0].State != "current" || results[0].HostLoadState != "unknown" {
		t.Fatalf("repeat sync was not current/unknown: %+v, %v", results, err)
	}
	if _, err := verifyOwned(folder); err != nil {
		t.Fatal(err)
	}
}

func TestSyncContinuesPastConflictsAndPreservesManualUninstall(t *testing.T) {
	base := testOptions(t)
	var options []Options
	for _, name := range []string{"edited", "missing", "updatable"} {
		opts := base
		opts.SkillsDir = filepath.Join(filepath.Dir(base.SkillsDir), name)
		opts.Endpoint = name
		if _, err := Skill(opts); err != nil {
			t.Fatal(err)
		}
		options = append(options, opts)
	}
	editedPath := filepath.Join(options[0].SkillsDir, skillName, "SKILL.md")
	if err := os.WriteFile(editedPath, []byte("my local edits"), 0600); err != nil {
		t.Fatal(err)
	}
	missingPath := filepath.Join(options[1].SkillsDir, skillName)
	if err := os.RemoveAll(missingPath); err != nil {
		t.Fatal(err)
	}
	results, err := syncSkillsAtRevision(base.DataDir, "", sourceText("new rules"), SkillRevision+1)
	if err != nil || len(results) != 3 {
		t.Fatalf("unexpected sync outcome: %+v, %v", results, err)
	}
	states := make(map[string]string)
	for _, result := range results {
		states[result.EndpointID] = result.State
	}
	if states["edited"] != "conflict" || states["missing"] != "missing" || states["updatable"] != "updated" {
		t.Fatalf("wrong independent outcomes: %+v", states)
	}
	if string(mustRead(t, editedPath)) != "my local edits" {
		t.Fatal("local edit was overwritten")
	}
	if _, err := os.Lstat(missingPath); !os.IsNotExist(err) {
		t.Fatalf("removed skill was recreated: %v", err)
	}
}

func TestEndpointSyncCannotUpdateOtherHosts(t *testing.T) {
	first := testOptions(t)
	second := first
	second.Host, second.Endpoint = "claude", "claude-test"
	second.SkillsDir = filepath.Join(filepath.Dir(first.SkillsDir), "claude-skills")
	for _, opts := range []Options{first, second} {
		if _, err := Skill(opts); err != nil {
			t.Fatal(err)
		}
		if _, err := installSkill(opts, sourceText("old rules")); err != nil {
			t.Fatal(err)
		}
	}
	results, err := SyncEndpoint(first.DataDir, first.Endpoint)
	if err != nil || len(results) != 1 || results[0].EndpointID != first.Endpoint || results[0].State != "updated" {
		t.Fatalf("endpoint filter failed: %+v, %v", results, err)
	}
	if string(mustRead(t, filepath.Join(second.SkillsDir, skillName, "SKILL.md"))) != "old rules" {
		t.Fatal("endpoint update changed another assistant's files")
	}
	status, err := StatusEndpoint(first.DataDir, second.Endpoint)
	if err != nil || len(status) != 1 || status[0].State != "outdated" || status[0].HostLoadState != "unknown" {
		t.Fatalf("endpoint status was incorrect: %+v, %v", status, err)
	}
	if _, err := SyncEndpoint(first.DataDir, ""); err == nil {
		t.Fatal("empty endpoint silently enabled global synchronization")
	}
	if _, err := StatusEndpoint(first.DataDir, "invalid\nendpoint"); err == nil {
		t.Fatal("invalid endpoint filter accepted")
	}
}

func TestStatusDoesNotCreateDirectoriesOrRewriteFiles(t *testing.T) {
	opts := testOptions(t)
	results, err := Status(opts.DataDir)
	if err != nil || len(results) != 0 {
		t.Fatalf("empty status failed: %+v, %v", results, err)
	}
	if _, err := os.Stat(opts.DataDir); !os.IsNotExist(err) {
		t.Fatalf("read-only status created its data directory: %v", err)
	}
	if _, err := Skill(opts); err != nil {
		t.Fatal(err)
	}
	before := mustRead(t, filepath.Join(opts.DataDir, registryName))
	results, err = Status(opts.DataDir)
	if err != nil || len(results) != 1 || results[0].State != "current" || !strings.HasPrefix(results[0].DiskVersion, "sha256:") {
		t.Fatalf("status did not report actual disk version: %+v, %v", results, err)
	}
	if !bytes.Equal(before, mustRead(t, filepath.Join(opts.DataDir, registryName))) {
		t.Fatal("status changed registry contents")
	}
}

func TestRegistryRejectsMalformedOrRedirectedFilesBeforeInstall(t *testing.T) {
	for _, content := range []string{"{}", "null", `{"owner":"someone-else","version":1,"installations":[]}`, `{"owner":"agihub/skill-installer","version":2,"installations":[]}`, `{"owner":"agihub/skill-installer","version":1,"installations":[],"token":"unexpected"}`, `{"owner":"agihub/skill-installer","version":1,"installations":[]} {}`} {
		t.Run(content, func(t *testing.T) {
			opts := testOptions(t)
			if err := os.MkdirAll(opts.DataDir, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(opts.DataDir, registryName), []byte(content), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := Skill(opts); err == nil {
				t.Fatal("installation accepted an invalid registry")
			}
			if _, err := os.Stat(opts.SkillsDir); !os.IsNotExist(err) {
				t.Fatalf("invalid registry did not prevent mutation: %v", err)
			}
		})
	}
	t.Run("symlink", func(t *testing.T) {
		opts := testOptions(t)
		if err := os.MkdirAll(opts.DataDir, 0700); err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(filepath.Dir(opts.DataDir), "outside.json")
		original := []byte(`{"owner":"agihub/skill-installer","version":1,"installations":[]}`)
		if err := os.WriteFile(target, original, 0600); err != nil {
			t.Fatal(err)
		}
		symlinkOrSkip(t, target, filepath.Join(opts.DataDir, registryName))
		if _, err := Skill(opts); err == nil {
			t.Fatal("installation followed a registry symlink")
		}
		if !bytes.Equal(original, mustRead(t, target)) {
			t.Fatal("outside registry target was modified")
		}
	})
}

func TestRegistryLockPreventsConcurrentLostUpdates(t *testing.T) {
	opts := testOptions(t)
	unlock, err := lockRegistry(opts.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Skill(opts); err == nil || !strings.Contains(err.Error(), "registry lock") {
		t.Fatalf("contending install did not preserve transaction lock: %v", err)
	}
	if _, err := Sync(opts.DataDir); err == nil || !strings.Contains(err.Error(), "registry lock") {
		t.Fatalf("contending sync did not preserve transaction lock: %v", err)
	}
	if _, err := os.Stat(filepath.Join(opts.DataDir, ".skill-installations.lock")); err != nil {
		t.Fatal("contender removed another operation's lock")
	}
	unlock()
	if _, err := Skill(opts); err != nil {
		t.Fatalf("install failed after lock was released: %v", err)
	}
}

func TestSyncRefusesRegistryAndMetadataMismatch(t *testing.T) {
	opts := testOptions(t)
	if _, err := Skill(opts); err != nil {
		t.Fatal(err)
	}
	// Simulate a completed explicit rebind whose registry publication failed.
	// A later automatic sync must not restore the old routing behind the user.
	changed := opts
	changed.Endpoint = "new-native-binding"
	if _, err := installSkill(changed, sourceText("old rules")); err != nil {
		t.Fatal(err)
	}
	results, err := Sync(opts.DataDir)
	if err != nil || len(results) != 1 || results[0].State != "conflict" || !strings.Contains(results[0].Detail, "routing differs") {
		t.Fatalf("stale registry silently restored old routing: %+v, %v", results, err)
	}
	var metadata installation
	if err := json.Unmarshal(mustRead(t, filepath.Join(opts.SkillsDir, skillName, "installation.json")), &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata.EndpointID != changed.Endpoint {
		t.Fatal("automatic sync rebound the native assistant")
	}
}

func TestSyncRefusesReplacedUnmanagedAndSymlinkDestinations(t *testing.T) {
	for _, kind := range []string{"unmanaged folder", "linked folder", "linked skills root", "linked data root"} {
		t.Run(kind, func(t *testing.T) {
			opts := testOptions(t)
			folder, err := Skill(opts)
			if err != nil {
				t.Fatal(err)
			}
			outside := filepath.Join(filepath.Dir(opts.SkillsDir), "outside")
			if err := os.MkdirAll(outside, 0700); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "unmanaged folder":
				if err := os.RemoveAll(folder); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(folder, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(folder, "SKILL.md"), []byte("mine"), 0600); err != nil {
					t.Fatal(err)
				}
			case "linked folder":
				if err := os.RemoveAll(folder); err != nil {
					t.Fatal(err)
				}
				symlinkOrSkip(t, outside, folder)
			case "linked skills root":
				if err := os.RemoveAll(opts.SkillsDir); err != nil {
					t.Fatal(err)
				}
				symlinkOrSkip(t, outside, opts.SkillsDir)
			case "linked data root":
				if err := os.Rename(opts.DataDir, filepath.Join(outside, "data")); err != nil {
					t.Fatal(err)
				}
				symlinkOrSkip(t, filepath.Join(outside, "data"), opts.DataDir)
			}
			results, err := Sync(opts.DataDir)
			if kind == "linked data root" {
				if err == nil {
					t.Fatal("sync followed a linked registry root")
				}
				return
			}
			if err != nil || len(results) != 1 || results[0].State != "conflict" {
				t.Fatalf("replacement destination was not refused: %+v, %v", results, err)
			}
			if kind == "unmanaged folder" && string(mustRead(t, filepath.Join(folder, "SKILL.md"))) != "mine" {
				t.Fatal("sync overwrote unmanaged skill")
			}
			entries, err := os.ReadDir(outside)
			if err != nil || len(entries) != 0 {
				t.Fatalf("sync wrote through a symlink: %v, %v", entries, err)
			}
		})
	}
}

func TestUpdateOnlyModeCannotRecreateADeletedSkill(t *testing.T) {
	opts := testOptions(t)
	if _, err := installSkillMode(opts, sourceText("replacement"), false); err == nil {
		t.Fatal("update-only installation created a deleted/uninstalled skill")
	}
	if _, err := os.Stat(filepath.Join(opts.SkillsDir, skillName)); !os.IsNotExist(err) {
		t.Fatalf("update-only mode published a new skill folder: %v", err)
	}
}

func TestSyncPreservesRestrictedReportHelperAndOptionalHubBinary(t *testing.T) {
	opts := testOptions(t)
	opts.BinaryPath = filepath.Join(filepath.Dir(opts.BinaryPath), "workbuddy-report-only")
	opts.HubBinaryPath = filepath.Join(filepath.Dir(opts.BinaryPath), "agi-hub")
	folder, err := Skill(opts)
	if err != nil {
		t.Fatal(err)
	}
	var metadata map[string]string
	if err := json.Unmarshal(mustRead(t, filepath.Join(folder, "installation.json")), &metadata); err != nil {
		t.Fatal(err)
	}
	if len(metadata) != 5 || metadata["binary_path"] != opts.BinaryPath || metadata["hub_binary_path"] != opts.HubBinaryPath {
		t.Fatalf("optional Hub CLI overwrote or expanded report configuration: %+v", metadata)
	}
	registry, err := readRegistry(opts.DataDir)
	if err != nil || len(registry.Installations) != 1 || registry.Installations[0].HubBinaryPath != opts.HubBinaryPath {
		t.Fatalf("optional Hub CLI was not registered: %+v, %v", registry, err)
	}
	results, err := syncSkillsAtRevision(opts.DataDir, opts.Endpoint, sourceText("updated cooperation rules"), SkillRevision+1)
	if err != nil || len(results) != 1 || results[0].State != "updated" {
		t.Fatalf("optional helper update failed: %+v, %v", results, err)
	}
	var after installation
	if err := json.Unmarshal(mustRead(t, filepath.Join(folder, "installation.json")), &after); err != nil {
		t.Fatal(err)
	}
	if after.BinaryPath != opts.BinaryPath || after.HubBinaryPath != opts.HubBinaryPath {
		t.Fatalf("sync changed the report-only helper: %+v", after)
	}
	if _, err := verifyOwned(folder); err != nil {
		t.Fatal(err)
	}
	bad := opts
	bad.HubBinaryPath = "relative-hub"
	if _, err := Skill(bad); err == nil {
		t.Fatal("relative optional Hub CLI path accepted")
	}
}

func TestMonotonicApprovedSourcePreventsDowngradeAndSameRevisionReplacement(t *testing.T) {
	opts := testOptions(t)
	folder, err := Skill(opts)
	if err != nil {
		t.Fatal(err)
	}
	newSource := "newer released collaboration rules"
	results, err := syncSkillsAtRevision(opts.DataDir, "", sourceText(newSource), SkillRevision+1)
	if err != nil || len(results) != 1 || results[0].State != "updated" || results[0].SourceRevision != SkillRevision+1 || results[0].ApprovedRevision != SkillRevision+1 {
		t.Fatalf("new release was not approved and installed: %+v, %v", results, err)
	}
	beforeRegistry := mustRead(t, filepath.Join(opts.DataDir, registryName))
	beforeMetadata := mustRead(t, filepath.Join(folder, "installation.json"))
	if _, err := Sync(opts.DataDir); err == nil || !strings.Contains(err.Error(), "downgrade") {
		t.Fatalf("older binary was allowed to synchronize over approved rules: %v", err)
	}
	opts.Endpoint = "should-not-rebind"
	if _, err := Skill(opts); err == nil || !strings.Contains(err.Error(), "downgrade") {
		t.Fatalf("older binary was allowed to explicitly install over approved rules: %v", err)
	}
	if _, err := syncSkillsAtRevision(opts.DataDir, "", sourceText("different content with no revision bump"), SkillRevision+1); err == nil || !strings.Contains(err.Error(), "increment SkillRevision") {
		t.Fatalf("same revision with a different digest was allowed: %v", err)
	}
	if string(mustRead(t, filepath.Join(folder, "SKILL.md"))) != newSource || !bytes.Equal(beforeMetadata, mustRead(t, filepath.Join(folder, "installation.json"))) || !bytes.Equal(beforeRegistry, mustRead(t, filepath.Join(opts.DataDir, registryName))) {
		t.Fatal("rejected source changed skill files, routing or approved source")
	}
	status, err := Status(opts.DataDir)
	if err != nil || len(status) != 1 || status[0].State != "incompatible" || status[0].ApprovedRevision != SkillRevision+1 || status[0].DiskVersion != skillVersion([]byte(newSource)) || !strings.Contains(status[0].Detail, "downgrade") {
		t.Fatalf("status did not report incompatible source and actual disk evidence: %+v, %v", status, err)
	}
	// A later release can move forward and idempotently repeat its own version.
	results, err = syncSkillsAtRevision(opts.DataDir, "", sourceText("next release"), SkillRevision+2)
	if err != nil || len(results) != 1 || results[0].State != "updated" {
		t.Fatalf("later release could not upgrade: %+v, %v", results, err)
	}
	results, err = syncSkillsAtRevision(opts.DataDir, "", sourceText("next release"), SkillRevision+2)
	if err != nil || len(results) != 1 || results[0].State != "current" {
		t.Fatalf("approved release was not idempotent: %+v, %v", results, err)
	}
}

func TestLegacyRegistryAdoptsCurrentSourceWithoutRewritingSkill(t *testing.T) {
	opts := testOptions(t)
	folder, err := Skill(opts)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := readRegistry(opts.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	registry.SourceRevision, registry.SourceVersion = 0, ""
	if err := writeRegistry(opts.DataDir, &registry); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(folder, "SKILL.md")
	stamp := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes(path, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	results, err := Sync(opts.DataDir)
	if err != nil || len(results) != 1 || results[0].State != "current" || results[0].ApprovedRevision != SkillRevision {
		t.Fatalf("legacy source adoption failed: %+v, %v", results, err)
	}
	info, err := os.Stat(path)
	if err != nil || !info.ModTime().Equal(stamp) {
		t.Fatalf("legacy adoption unnecessarily rewrote skill: %v", err)
	}
}

func TestSourceApprovalMustPersistBeforeAnySkillUpdate(t *testing.T) {
	opts := testOptions(t)
	folder, err := Skill(opts)
	if err != nil {
		t.Fatal(err)
	}
	before := mustRead(t, filepath.Join(folder, "SKILL.md"))
	blockedRegistry := filepath.Join(opts.DataDir, registryName)
	_, err = syncSkillsAtRevision(opts.DataDir, "", func() ([]byte, error) {
		// Simulate a registry publication failure after the transaction read it.
		if err := os.Remove(blockedRegistry); err != nil {
			return nil, err
		}
		if err := os.Mkdir(blockedRegistry, 0700); err != nil {
			return nil, err
		}
		return []byte("new release must not reach skill files"), nil
	}, SkillRevision+1)
	if err == nil || !strings.Contains(err.Error(), "before installation") {
		t.Fatalf("source approval write failure was not returned: %v", err)
	}
	if !bytes.Equal(before, mustRead(t, filepath.Join(folder, "SKILL.md"))) {
		t.Fatal("skill changed before newer source approval was persisted")
	}
}
