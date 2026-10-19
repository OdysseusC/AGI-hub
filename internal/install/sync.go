package install

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"unicode"
)

const registryName = "skill-installations.json"

// SyncResult describes files on disk only. HostLoadState remains unknown:
// updating a skill file does not prove that a running native session reloaded it.
type SyncResult struct {
	Host             string `json:"host"`
	EndpointID       string `json:"endpoint_id"`
	SkillsDir        string `json:"skills_dir"`
	SkillPath        string `json:"skill_path"`
	DesiredVersion   string `json:"desired_version"`
	SourceRevision   int    `json:"source_revision"`
	ApprovedRevision int    `json:"approved_revision,omitempty"`
	ApprovedVersion  string `json:"approved_version,omitempty"`
	DiskVersion      string `json:"disk_version,omitempty"`
	State            string `json:"state"`
	HostLoadState    string `json:"host_load_state"`
	Detail           string `json:"detail,omitempty"`
}

// installationRecord includes only explicit installer inputs, never host
// credentials or session history. Registry entries do not authorize discovery
// or installation into any other directory.
type installationRecord struct {
	Host          string `json:"host"`
	EndpointID    string `json:"endpoint_id"`
	BinaryPath    string `json:"binary_path"`
	HubBinaryPath string `json:"hub_binary_path,omitempty"`
	SkillsDir     string `json:"skills_dir"`
}

type installationRegistry struct {
	Owner          string               `json:"owner"`
	Version        int                  `json:"version"`
	SourceRevision int                  `json:"source_revision,omitempty"`
	SourceVersion  string               `json:"source_version,omitempty"`
	Installations  []installationRecord `json:"installations"`
}

// Sync updates only previously registered, unmodified managed installations.
// Missing skill folders are reported and are never recreated: removing a skill
// is treated as an intentional uninstall until the user installs it explicitly.
// One conflicting host does not prevent other registered hosts from updating.
func Sync(dataDir string) ([]SyncResult, error) {
	return syncSkills(dataDir, "", embeddedSkill)
}

// SyncEndpoint restricts updates to an endpoint's explicitly registered skill
// locations. An assistant uses this instead of updating unrelated hosts.
func SyncEndpoint(dataDir, endpoint string) ([]SyncResult, error) {
	if err := validateEndpointFilter(endpoint); err != nil {
		return nil, err
	}
	return syncSkills(dataDir, endpoint, embeddedSkill)
}

func syncSkills(dataDir, endpoint string, source func() ([]byte, error)) ([]SyncResult, error) {
	return syncSkillsAtRevision(dataDir, endpoint, source, SkillRevision)
}

func syncSkillsAtRevision(dataDir, endpoint string, source func() ([]byte, error), revision int) ([]SyncResult, error) {
	dataDir, err := normalizedDataDir(dataDir)
	if err != nil {
		return nil, err
	}
	unlock, err := lockRegistry(dataDir)
	if err != nil {
		return nil, err
	}
	defer unlock()
	registry, err := readRegistry(dataDir)
	if err != nil {
		return nil, err
	}
	body, err := source()
	if err != nil {
		return nil, fmt.Errorf("read embedded skill: %w", err)
	}
	if err := compatibleSource(registry, revision, skillVersion(body)); err != nil {
		return nil, err
	}
	selected := false
	for _, record := range registry.Installations {
		if endpoint == "" || record.EndpointID == endpoint {
			selected = true
			break
		}
	}
	if selected {
		if err := approveSource(dataDir, &registry, revision, skillVersion(body)); err != nil {
			return nil, err
		}
	}
	results := make([]SyncResult, 0, len(registry.Installations))
	for _, record := range registry.Installations {
		if endpoint != "" && record.EndpointID != endpoint {
			continue
		}
		result := inspectInstallation(dataDir, record, skillVersion(body))
		result.SourceRevision, result.ApprovedRevision, result.ApprovedVersion = revision, registry.SourceRevision, registry.SourceVersion
		if result.State == "outdated" {
			_, err := installSkillMode(record.options(dataDir), func() ([]byte, error) { return body, nil }, false)
			if err != nil {
				result.State = "error"
				result.Detail = err.Error()
			} else {
				result.State = "updated"
				result.DiskVersion = skillVersion(body)
			}
		}
		results = append(results, result)
	}
	return results, nil
}

// Status inspects registered installations without writing files, creating
// directories or invoking an assistant. It does not infer native host loading.
func Status(dataDir string) ([]SyncResult, error) {
	return statusSkills(dataDir, "")
}

// StatusEndpoint inspects only one endpoint's registered locations.
func StatusEndpoint(dataDir, endpoint string) ([]SyncResult, error) {
	if err := validateEndpointFilter(endpoint); err != nil {
		return nil, err
	}
	return statusSkills(dataDir, endpoint)
}

func statusSkills(dataDir, endpoint string) ([]SyncResult, error) {
	dataDir, err := normalizedDataDir(dataDir)
	if err != nil {
		return nil, err
	}
	registry, err := readRegistry(dataDir)
	if err != nil {
		return nil, err
	}
	_, desired, err := CurrentSkill()
	if err != nil {
		return nil, err
	}
	results := make([]SyncResult, 0, len(registry.Installations))
	sourceErr := compatibleSource(registry, SkillRevision, desired)
	for _, record := range registry.Installations {
		if endpoint != "" && record.EndpointID != endpoint {
			continue
		}
		result := inspectInstallation(dataDir, record, desired)
		result.SourceRevision, result.ApprovedRevision, result.ApprovedVersion = SkillRevision, registry.SourceRevision, registry.SourceVersion
		if sourceErr != nil {
			result.State, result.Detail = "incompatible", sourceErr.Error()
		}
		results = append(results, result)
	}
	return results, nil
}

func inspectInstallation(dataDir string, record installationRecord, desired string) SyncResult {
	folder := filepath.Join(record.SkillsDir, skillName)
	result := SyncResult{Host: record.Host, EndpointID: record.EndpointID,
		SkillsDir: record.SkillsDir, SkillPath: folder, DesiredVersion: desired,
		State: "conflict", HostLoadState: "unknown"}
	if err := checkDirectoryPath(record.SkillsDir); err != nil {
		result.Detail = err.Error()
		return result
	}
	info, err := os.Lstat(folder)
	if errors.Is(err, os.ErrNotExist) {
		result.State = "missing"
		result.Detail = "registered skill folder is absent; explicit installation is required to restore it"
		return result
	}
	if err != nil {
		result.State = "error"
		result.Detail = err.Error()
		return result
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		result.Detail = "registered skill destination must be a real directory"
		return result
	}
	if body, err := readRegularFile(filepath.Join(folder, "SKILL.md")); err == nil {
		result.DiskVersion = skillVersion(body)
	}
	if _, err := verifyOwned(folder); err != nil {
		result.Detail = err.Error()
		return result
	}
	metadata, err := readRegularFile(filepath.Join(folder, "installation.json"))
	if err != nil {
		result.Detail = err.Error()
		return result
	}
	var installed installation
	if err := json.Unmarshal(metadata, &installed); err != nil || installed != (installation{
		Host: record.Host, EndpointID: record.EndpointID, BinaryPath: record.BinaryPath, DataDir: dataDir,
		HubBinaryPath: record.HubBinaryPath,
	}) {
		result.Detail = "registered routing differs from installed metadata; explicit installation is required to reconcile it"
		return result
	}
	if result.DiskVersion == desired {
		result.State = "current"
	} else {
		result.State = "outdated"
	}
	return result
}

func skillVersion(body []byte) string { return "sha256:" + checksum(body) }

func compatibleSource(registry installationRegistry, revision int, version string) error {
	if revision <= 0 || !validSkillVersion(version) {
		return errors.New("invalid collaboration rules source revision or digest")
	}
	if registry.SourceRevision > revision {
		return fmt.Errorf("refuse collaboration rules downgrade: approved revision %d is newer than this binary's revision %d; update the Hub CLI", registry.SourceRevision, revision)
	}
	if registry.SourceRevision == revision && registry.SourceVersion != version {
		return fmt.Errorf("refuse different collaboration rules at approved revision %d; a changed release must increment SkillRevision", revision)
	}
	return nil
}

func validSkillVersion(version string) bool {
	if !strings.HasPrefix(version, "sha256:") || len(version) != len("sha256:")+64 {
		return false
	}
	_, err := hex.DecodeString(strings.TrimPrefix(version, "sha256:"))
	return err == nil
}

// approveSource persists the monotonic release decision before touching skill
// files. If a later host update fails, an older process still cannot downgrade
// any files that were already successfully updated in this transaction.
func approveSource(dataDir string, registry *installationRegistry, revision int, version string) error {
	if err := compatibleSource(*registry, revision, version); err != nil {
		return err
	}
	if registry.SourceRevision == revision && registry.SourceVersion == version {
		return nil
	}
	registry.SourceRevision, registry.SourceVersion = revision, version
	if err := writeRegistry(dataDir, registry); err != nil {
		return fmt.Errorf("record approved collaboration rules source before installation: %w", err)
	}
	return nil
}

func validateEndpointFilter(endpoint string) error {
	if strings.TrimSpace(endpoint) == "" || len(endpoint) > 128 || strings.IndexFunc(endpoint, unicode.IsControl) != -1 {
		return errors.New("a nonempty endpoint ID of at most 128 bytes is required")
	}
	return nil
}

func (record installationRecord) options(dataDir string) Options {
	return Options{Host: record.Host, Endpoint: record.EndpointID, BinaryPath: record.BinaryPath,
		HubBinaryPath: record.HubBinaryPath,
		DataDir:       dataDir, SkillsDir: record.SkillsDir}
}

func normalizedDataDir(dataDir string) (string, error) {
	if err := absolutePath("data directory", dataDir); err != nil {
		return "", err
	}
	dataDir = filepath.Clean(dataDir)
	if err := checkDirectoryPath(dataDir); err != nil {
		return "", err
	}
	return dataDir, nil
}

func lockRegistry(dataDir string) (func(), error) {
	dataDir, err := normalizedDataDir(dataDir)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return nil, fmt.Errorf("create skill registry directory: %w", err)
	}
	if err := checkDirectoryPath(dataDir); err != nil {
		return nil, err
	}
	path := filepath.Join(dataDir, ".skill-installations.lock")
	lock, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return nil, fmt.Errorf("acquire managed skill registry lock (inspect an existing lock before retrying): %w", err)
	}
	if err := lock.Close(); err != nil {
		_ = os.Remove(path)
		return nil, err
	}
	return func() { _ = os.Remove(path) }, nil
}

func readRegistry(dataDir string) (installationRegistry, error) {
	registry := installationRegistry{Owner: owner, Version: 1, Installations: []installationRecord{}}
	if err := checkDirectoryPath(dataDir); err != nil {
		return registry, err
	}
	body, err := readRegularFile(filepath.Join(dataDir, registryName))
	if errors.Is(err, os.ErrNotExist) {
		return registry, nil
	}
	if err != nil {
		return registry, fmt.Errorf("read managed skill registry: %w", err)
	}
	if len(body) > 1<<20 {
		return registry, errors.New("managed skill registry exceeds 1 MiB")
	}
	// Do not let omitted ownership fields inherit the defaults used for a
	// genuinely absent registry.
	registry = installationRegistry{}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&registry); err != nil {
		return registry, fmt.Errorf("invalid managed skill registry: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return registry, errors.New("invalid trailing managed skill registry data")
	}
	if registry.Owner != owner || registry.Version != 1 || registry.Installations == nil || len(registry.Installations) > 256 {
		return registry, errors.New("unsupported managed skill registry ownership, version or entry count")
	}
	if registry.SourceRevision < 0 || (registry.SourceRevision == 0 && registry.SourceVersion != "") || (registry.SourceRevision > 0 && !validSkillVersion(registry.SourceVersion)) {
		return registry, errors.New("managed skill registry contains an invalid approved source revision or digest")
	}
	seen := make(map[string]bool, len(registry.Installations))
	for _, record := range registry.Installations {
		options, err := normalizeOptions(record.options(dataDir))
		if err != nil || options.Host != record.Host || options.BinaryPath != record.BinaryPath || options.HubBinaryPath != record.HubBinaryPath || options.SkillsDir != record.SkillsDir {
			return registry, errors.New("managed skill registry contains a non-normalized installation")
		}
		key := installationPathKey(record.SkillsDir)
		if seen[key] {
			return registry, errors.New("managed skill registry contains duplicate installation paths")
		}
		seen[key] = true
	}
	return registry, nil
}

// registerInstallation runs while the registry lock is held. Reinstalling an
// unchanged location is a no-op, including the registry's modification time.
func registerInstallation(dataDir string, registry *installationRegistry, options Options) error {
	record := installationRecord{Host: options.Host, EndpointID: options.Endpoint,
		HubBinaryPath: options.HubBinaryPath,
		BinaryPath:    options.BinaryPath, SkillsDir: options.SkillsDir}
	updated := false
	for index, previous := range registry.Installations {
		if installationPathKey(previous.SkillsDir) != installationPathKey(record.SkillsDir) {
			continue
		}
		if previous == record {
			return nil
		}
		registry.Installations[index] = record
		updated = true
		break
	}
	if !updated {
		if len(registry.Installations) >= 256 {
			return errors.New("managed skill registry already contains 256 installations")
		}
		registry.Installations = append(registry.Installations, record)
	}
	sort.Slice(registry.Installations, func(i, j int) bool {
		return registry.Installations[i].SkillsDir < registry.Installations[j].SkillsDir
	})
	return writeRegistry(dataDir, registry)
}

func writeRegistry(dataDir string, registry *installationRegistry) error {
	body, err := json.MarshalIndent(registry, "", "  ")
	if err != nil {
		return err
	}
	return replaceFile(dataDir, registryName, append(body, '\n'), 0600)
}

func installationPathKey(path string) string {
	if runtime.GOOS == "windows" {
		return strings.ToLower(path)
	}
	return path
}
