// Package install installs the AGI Hub protocol skill into an explicitly
// selected native assistant's skill directory. Installation is not transport
// integration or proof that a native session can be woken.
package install

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
)

const (
	// SkillRevision must increase whenever the released embedded rules change.
	// It prevents an older installed CLI from replacing newer approved rules.
	// This release counter is independent of manifest and registry schemas.
	SkillRevision = 7

	skillName    = "agi-hub-client"
	manifestName = ".agi-hub-manifest.json"
	owner        = "agihub/skill-installer"
)

//go:embed skills/agi-hub-client/SKILL.en.md
var skillFiles embed.FS

// Options selects one user's host and endpoint. The caller must ensure the
// user has authorized installation for this host and endpoint.
type Options struct {
	Host       string
	Endpoint   string
	BinaryPath string
	// HubBinaryPath is optional when BinaryPath is a restricted, endpoint-scoped
	// report helper rather than the general Hub CLI.
	HubBinaryPath string
	DataDir       string
	SkillsDir     string
}

// installation deliberately contains no credentials or copied host config.
type installation struct {
	Host          string `json:"host"`
	EndpointID    string `json:"endpoint_id"`
	BinaryPath    string `json:"binary_path"`
	HubBinaryPath string `json:"hub_binary_path,omitempty"`
	DataDir       string `json:"data_dir"`
}

type manifest struct {
	Owner   string            `json:"owner"`
	Version int               `json:"version"`
	Files   map[string]string `json:"files"`
}

// Skill returns the installed skill folder. Repeating an unchanged installation
// is a no-op. It refuses to overwrite unmanaged or locally edited files.
func Skill(options Options) (string, error) {
	options, err := normalizeOptions(options)
	if err != nil {
		return "", err
	}
	unlock, err := lockRegistry(options.DataDir)
	if err != nil {
		return "", err
	}
	defer unlock()
	registry, err := readRegistry(options.DataDir)
	if err != nil {
		return "", err
	}
	body, desiredVersion, err := CurrentSkill()
	if err != nil {
		return "", err
	}
	if err := approveSource(options.DataDir, &registry, SkillRevision, desiredVersion); err != nil {
		return "", err
	}
	folder, err := installSkill(options, func() ([]byte, error) { return body, nil })
	if err != nil {
		return "", err
	}
	if err := registerInstallation(options.DataDir, &registry, options); err != nil {
		return folder, fmt.Errorf("skill installed but managed update registration failed: %w", err)
	}
	return folder, nil
}

func embeddedSkill() ([]byte, error) {
	return skillFiles.ReadFile("skills/agi-hub-client/SKILL.en.md")
}

// CurrentSkill returns this binary's collaboration rules and their content
// digest. The digest is a skill version, independent of the manifest schema.
func CurrentSkill() ([]byte, string, error) {
	body, err := embeddedSkill()
	if err != nil {
		return nil, "", err
	}
	return body, skillVersion(body), nil
}

// installSkill accepts a source function so upgrade behavior is testable without
// altering the embedded production skill.
func installSkill(options Options, source func() ([]byte, error)) (string, error) {
	return installSkillMode(options, source, true)
}

func installSkillMode(options Options, source func() ([]byte, error), allowCreate bool) (string, error) {
	options, err := normalizeOptions(options)
	if err != nil {
		return "", err
	}
	if err := checkDirectoryPath(options.SkillsDir); err != nil {
		return "", err
	}
	if err := os.MkdirAll(options.SkillsDir, 0700); err != nil {
		return "", fmt.Errorf("create skills directory: %w", err)
	}
	if err := checkDirectoryPath(options.SkillsDir); err != nil {
		return "", err
	}

	// Cooperating installers serialize mutations. A stale lock after a process
	// crash is left in place for inspection rather than stealing another lock.
	lockPath := filepath.Join(options.SkillsDir, "."+skillName+"-install.lock")
	lock, err := os.OpenFile(lockPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return "", fmt.Errorf("acquire skill install lock (inspect an existing lock before retrying): %w", err)
	}
	if err := lock.Close(); err != nil {
		_ = os.Remove(lockPath)
		return "", err
	}
	defer os.Remove(lockPath)

	folder := filepath.Join(options.SkillsDir, skillName)
	if err := directChild(options.SkillsDir, folder); err != nil {
		return "", err
	}
	skill, err := source()
	if err != nil {
		return "", fmt.Errorf("read embedded skill: %w", err)
	}
	metadata, err := json.MarshalIndent(installation{
		Host: options.Host, EndpointID: options.Endpoint,
		BinaryPath: options.BinaryPath, DataDir: options.DataDir,
		HubBinaryPath: options.HubBinaryPath,
	}, "", "  ")
	if err != nil {
		return "", err
	}
	policy, err := communicationPolicy(options)
	if err != nil {
		return "", err
	}
	files := map[string][]byte{"SKILL.md": skill, "installation.json": append(metadata, '\n'), policyName: policy}

	info, err := os.Lstat(folder)
	if errors.Is(err, os.ErrNotExist) {
		if !allowCreate {
			return "", errors.New("registered skill folder disappeared; explicit installation is required to restore it")
		}
		if err := createSkill(options.SkillsDir, folder, files); err != nil {
			return "", err
		}
		return folder, nil
	}
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return "", errors.New("skill destination must be a real directory, not a symlink or file")
	}
	previous, err := verifyOwned(folder)
	if err != nil {
		return "", err
	}
	if err := checkNewManagedFiles(folder, previous, files); err != nil {
		return "", err
	}
	unchanged := true
	for name, content := range files {
		if previous.Files[name] != checksum(content) {
			unchanged = false
		}
	}
	if unchanged {
		return folder, nil
	}
	// Recheck before mutation. This lock coordinates our installers, not hostile
	// processes running under the same OS account.
	if err := checkDirectoryPath(folder); err != nil {
		return "", err
	}
	if _, err := verifyOwned(folder); err != nil {
		return "", err
	}
	if err := checkNewManagedFiles(folder, previous, files); err != nil {
		return "", err
	}
	for _, name := range sortedNames(files) {
		if previous.Files[name] == checksum(files[name]) {
			continue
		}
		if err := replaceFile(folder, name, files[name], fileMode(name)); err != nil {
			return "", err
		}
	}
	encoded, err := encodeManifest(files)
	if err != nil {
		return "", err
	}
	if err := replaceFile(folder, manifestName, encoded, 0600); err != nil {
		return "", err
	}
	return folder, nil
}

func normalizeOptions(options Options) (Options, error) {
	options.Host = strings.ToLower(strings.TrimSpace(options.Host))
	if !validHost(options.Host) {
		return options, errors.New("host must use lowercase letters, digits, or hyphens and be at most 64 characters")
	}
	if strings.TrimSpace(options.Endpoint) == "" || len(options.Endpoint) > 128 || strings.IndexFunc(options.Endpoint, unicode.IsControl) != -1 {
		return options, errors.New("a nonempty endpoint ID of at most 128 bytes is required")
	}
	for label, path := range map[string]string{"binary path": options.BinaryPath, "data directory": options.DataDir} {
		if err := absolutePath(label, path); err != nil {
			return options, err
		}
	}
	options.BinaryPath = filepath.Clean(options.BinaryPath)
	if options.HubBinaryPath != "" {
		if err := absolutePath("hub binary path", options.HubBinaryPath); err != nil {
			return options, err
		}
		options.HubBinaryPath = filepath.Clean(options.HubBinaryPath)
	}
	options.DataDir = filepath.Clean(options.DataDir)
	if options.SkillsDir == "" {
		var hostDir string
		switch options.Host {
		case "codex":
			hostDir = ".codex"
		case "workbuddy":
			hostDir = ".workbuddy"
		case "claude":
			hostDir = ".claude"
		default:
			return options, errors.New("this host requires an explicit skills directory; no discovery path is assumed")
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return options, err
		}
		options.SkillsDir = filepath.Join(home, hostDir, "skills")
	}
	if err := absolutePath("skills directory", options.SkillsDir); err != nil {
		return options, err
	}
	options.SkillsDir = filepath.Clean(options.SkillsDir)
	if filepath.Dir(options.SkillsDir) == options.SkillsDir {
		return options, errors.New("the filesystem root cannot be the skills directory")
	}
	return options, nil
}

func validHost(host string) bool {
	if len(host) == 0 || len(host) > 64 || host[0] == '-' {
		return false
	}
	for _, c := range host {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
			return false
		}
	}
	return true
}

func absolutePath(label, path string) error {
	if !filepath.IsAbs(path) || strings.IndexByte(path, 0) != -1 {
		return fmt.Errorf("%s must be an absolute local path", label)
	}
	for _, component := range strings.Split(filepath.ToSlash(path), "/") {
		if component == ".." {
			return fmt.Errorf("%s cannot contain parent traversal", label)
		}
	}
	return nil
}

// checkDirectoryPath rejects symlinks in the destination or any existing parent
// component. The same user's concurrently changing filesystem is not a sandbox.
func checkDirectoryPath(path string) error {
	for current := path; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err == nil {
			if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
				return fmt.Errorf("skills path component must be a real directory: %s", current)
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if filepath.Dir(current) == current {
			return nil
		}
	}
}

func directChild(root, path string) error {
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." || filepath.Dir(rel) != "." || filepath.IsAbs(rel) || rel == ".." {
		return errors.New("managed skill path escapes its directory")
	}
	return nil
}

func createSkill(root, folder string, files map[string][]byte) error {
	stage, err := os.MkdirTemp(root, "."+skillName+"-stage-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	for _, name := range sortedNames(files) {
		if err := replaceFile(stage, name, files[name], fileMode(name)); err != nil {
			return err
		}
	}
	encoded, err := encodeManifest(files)
	if err != nil {
		return err
	}
	if err := replaceFile(stage, manifestName, encoded, 0600); err != nil {
		return err
	}
	if err := checkDirectoryPath(root); err != nil {
		return err
	}
	if _, err := os.Lstat(folder); !errors.Is(err, os.ErrNotExist) {
		return errors.New("skill destination appeared during installation; no existing folder was replaced")
	}
	if err := os.Rename(stage, folder); err != nil {
		return fmt.Errorf("publish new skill folder: %w", err)
	}
	return nil
}

func verifyOwned(folder string) (manifest, error) {
	var m manifest
	data, err := readRegularFile(filepath.Join(folder, manifestName))
	if err != nil {
		return m, fmt.Errorf("refuse to overwrite skill without a valid ownership manifest: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&m); err != nil {
		return m, fmt.Errorf("invalid skill ownership manifest: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return m, errors.New("invalid trailing ownership manifest data")
	}
	legacy := m.Version == 1 && len(m.Files) == 2
	current := m.Version == 2 && len(m.Files) == 3 && m.Files[policyName] != ""
	if m.Owner != owner || (!legacy && !current) || m.Files["SKILL.md"] == "" || m.Files["installation.json"] == "" {
		return m, errors.New("skill is not owned by this installer or uses an unsupported ownership manifest")
	}
	for name, expected := range m.Files {
		if name != "SKILL.md" && name != "installation.json" && !(current && name == policyName) {
			return m, errors.New("ownership manifest includes an unmanaged path")
		}
		data, err := readRegularFile(filepath.Join(folder, name))
		if err != nil {
			return m, fmt.Errorf("refuse to overwrite missing or non-regular managed file %s: %w", name, err)
		}
		if checksum(data) != expected {
			return m, fmt.Errorf("refuse to overwrite locally edited managed file: %s", name)
		}
	}
	return m, nil
}

// A new installer-owned filename cannot silently take ownership of a file the
// user placed beside an older installation.
func checkNewManagedFiles(folder string, previous manifest, files map[string][]byte) error {
	for name := range files {
		if _, managed := previous.Files[name]; managed {
			continue
		}
		if _, err := os.Lstat(filepath.Join(folder, name)); !errors.Is(err, os.ErrNotExist) {
			if err != nil {
				return err
			}
			return fmt.Errorf("refuse to overwrite unmanaged file: %s", name)
		}
	}
	return nil
}

func readRegularFile(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("managed file must be regular, not a symlink or directory")
	}
	return os.ReadFile(path)
}

func encodeManifest(files map[string][]byte) ([]byte, error) {
	m := manifest{Owner: owner, Version: 2, Files: make(map[string]string, len(files))}
	for name, content := range files {
		m.Files[name] = checksum(content)
	}
	encoded, err := json.MarshalIndent(m, "", "  ")
	return append(encoded, '\n'), err
}

func checksum(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

func sortedNames(files map[string][]byte) []string {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func fileMode(name string) os.FileMode {
	if name == "SKILL.md" {
		return 0644
	}
	return 0600
}

func replaceFile(folder, name string, content []byte, mode os.FileMode) error {
	path := filepath.Join(folder, name)
	if err := directChild(folder, path); err != nil {
		return err
	}
	if info, err := os.Lstat(path); err == nil {
		if !info.Mode().IsRegular() {
			return errors.New("refuse to replace a symlink or non-regular managed file")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	f, err := os.CreateTemp(folder, "."+name+"-")
	if err != nil {
		return err
	}
	temp := f.Name()
	defer os.Remove(temp)
	if err := f.Chmod(mode); err != nil {
		_ = f.Close()
		return err
	}
	if _, err := f.Write(content); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	// Rename replacement can be blocked by open files on Windows. Preserve the
	// old destination and return the error; never delete it as a fallback.
	if err := os.Rename(temp, path); err != nil {
		return fmt.Errorf("replace managed file %s (close files held by the host before retrying): %w", name, err)
	}
	return nil
}
