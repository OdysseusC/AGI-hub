// Package autostart restores explicitly selected local processes at user login.
// It never consumes mail, changes session state, or grants native permissions.
package autostart

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

type Client struct {
	Endpoint     string            `json:"endpoint"`
	Enabled      bool              `json:"enabled"`
	Command      string            `json:"command,omitempty"`
	Args         []string          `json:"args,omitempty"`
	Cwd          string            `json:"cwd,omitempty"`
	Environment  map[string]string `json:"environment,omitempty"`
	RequiresHost bool              `json:"requires_host"`
}

type Plan struct {
	Version int      `json:"version"`
	Enabled bool     `json:"enabled"`
	Binary  string   `json:"binary,omitempty"`
	Clients []Client `json:"clients"`
}

var planMu sync.Mutex
var endpointPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)

const maxStartupJSONBytes = 1 << 20
const planLockName = ".agi-hub-startup-config.lock"

var errStartupJSONChanged = errors.New("startup file changed while opening it; retry")

func RegistrationFor(dir, binary string) Registration {
	digest := sha256.Sum256([]byte(filepath.Clean(dir)))
	return Registration{ID: fmt.Sprintf("com.agihub.startup.%x", digest[:8]), Binary: binary, DataDir: dir, LogDir: filepath.Join(dir, "logs", "autostart")}
}

func readJSON[T any](path string, value *T) error {
	deadline := time.Now().Add(200 * time.Millisecond)
	changedAttempts := 0
	for {
		// A failed decode may already have populated maps and slices. Never
		// expose those partial values or merge them into a later snapshot.
		var fresh T
		err := readJSONAttempt(path, &fresh)
		if err == nil {
			*value = fresh
			return nil
		}
		if errors.Is(err, errStartupJSONChanged) {
			changedAttempts++
			if changedAttempts < 3 {
				continue
			}
			return err
		}
		if !startupReadSharingViolation(err) {
			return err
		}
		// ReplaceFileW briefly opens its replacement without sharing. Retry
		// only that Windows conflict; permission/path/format errors fail closed.
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return err
		}
		delay := 10 * time.Millisecond
		if remaining < delay {
			delay = remaining
		}
		time.Sleep(delay)
		if !time.Now().Before(deadline) {
			return err
		}
	}
}

func readJSONAttempt(path string, value any) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() > maxStartupJSONBytes {
		return errors.New("startup file must be a regular file under 1 MiB")
	}
	file, err := openStartupReadFile(path)
	if err != nil {
		return err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil {
		return err
	}
	current, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !opened.Mode().IsRegular() || !current.Mode().IsRegular() {
		return errors.New("startup file path changed or is not a regular file")
	}
	// An atomic writer may replace the path between either stat and open.
	// Either complete, regular-file snapshot is safe to read.
	if !os.SameFile(info, opened) && !os.SameFile(opened, current) {
		return errStartupJSONChanged
	}
	b, err := io.ReadAll(io.LimitReader(file, maxStartupJSONBytes+1))
	if err != nil {
		return err
	}
	if len(b) > maxStartupJSONBytes || !utf8.Valid(b) {
		return errors.New("startup file must contain valid UTF-8 under 1 MiB")
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err := d.Decode(value); err != nil {
		return err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return errors.New("startup file must contain exactly one JSON object")
	}
	return nil
}

func writeJSON(path string, value any) error {
	staged, err := stageJSON(path, value)
	if err != nil {
		return err
	}
	defer staged.cleanup()
	return staged.commit()
}

type stagedStartupJSON struct {
	destination       string
	temporary         string
	previous          os.FileInfo
	previousSHA       [sha256.Size]byte
	preserveTemporary bool
}

func stageJSON(path string, value any) (*stagedStartupJSON, error) {
	previous, err := os.Lstat(path)
	if err == nil && !previous.Mode().IsRegular() {
		return nil, errors.New("startup destination must be a regular file")
	} else if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	var previousSHA [sha256.Size]byte
	if previous != nil {
		previousSHA, err = startupJSONFingerprint(path, previous)
		if err != nil {
			return nil, err
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, err
	}
	if len(b)+1 > maxStartupJSONBytes {
		return nil, errors.New("startup JSON must remain under 1 MiB")
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".startup-*")
	if err != nil {
		return nil, err
	}
	staged := &stagedStartupJSON{destination: path, temporary: f.Name(), previous: previous, previousSHA: previousSHA}
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(append(b, '\n'))
	}
	if err == nil {
		err = f.Sync()
	}
	err = errors.Join(err, f.Close())
	if err != nil {
		staged.cleanup()
		return nil, err
	}
	return staged, nil
}

func (s *stagedStartupJSON) cleanup() {
	if !s.preserveTemporary {
		_ = os.Remove(s.temporary)
	}
}

func (s *stagedStartupJSON) commit() error {
	current, err := os.Lstat(s.destination)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if (s.previous == nil && err == nil) || (s.previous != nil && (err != nil || !current.Mode().IsRegular() || !os.SameFile(s.previous, current))) {
		return errors.New("startup destination changed during configuration; retry")
	}
	if s.previous != nil {
		currentSHA, err := startupJSONFingerprint(s.destination, s.previous)
		if err != nil {
			return err
		}
		// Inode numbers may be reused after unlink/recreate, and an editor can
		// change bytes without replacing the inode. Identity is not a content
		// version, even when file size and modification time also match.
		if currentSHA != s.previousSHA {
			return errors.New("startup destination content changed during configuration; retry")
		}
	}
	if err := replaceStartupJSONFile(s.temporary, s.destination, s.previous != nil); err != nil {
		s.preserveTemporary = true
		return fmt.Errorf("replace startup JSON; staged file retained for inspection: %w", err)
	}
	return nil
}

func startupJSONFingerprint(path string, expected os.FileInfo) ([sha256.Size]byte, error) {
	var fingerprint [sha256.Size]byte
	file, err := openStartupReadFile(path)
	if err != nil {
		return fingerprint, err
	}
	defer file.Close()
	before, err := file.Stat()
	if err != nil {
		return fingerprint, err
	}
	if !before.Mode().IsRegular() || !os.SameFile(expected, before) || expected.Size() != before.Size() || !expected.ModTime().Equal(before.ModTime()) {
		return fingerprint, errors.New("startup destination changed during configuration; retry")
	}
	digest := sha256.New()
	size, err := io.Copy(digest, io.LimitReader(file, maxStartupJSONBytes+1))
	if err != nil {
		return fingerprint, err
	}
	if size > maxStartupJSONBytes {
		return fingerprint, errors.New("startup destination must remain under 1 MiB")
	}
	after, err := file.Stat()
	if err != nil {
		return fingerprint, err
	}
	current, err := os.Lstat(path)
	if err != nil {
		return fingerprint, err
	}
	if !current.Mode().IsRegular() || !os.SameFile(before, current) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		return fingerprint, errors.New("startup destination changed while reading its contents; retry")
	}
	copy(fingerprint[:], digest.Sum(nil))
	return fingerprint, nil
}

func Load(dir string) (Plan, error) {
	p := Plan{Version: 1, Clients: []Client{}}
	if err := validatePlanDirectory(dir); err != nil {
		return p, err
	}
	err := readJSON(filepath.Join(dir, "startup.json"), &p)
	if os.IsNotExist(err) {
		return p, nil
	}
	if err != nil {
		return p, err
	}
	return p, Validate(p)
}

func Validate(p Plan) error {
	if p.Version != 1 || len(p.Clients) > 32 {
		return errors.New("unsupported startup version or too many Clients")
	}
	if p.Binary != "" && (!filepath.IsAbs(p.Binary) || !validPlanValue(p.Binary)) {
		return errors.New("startup binary must be an absolute path")
	}
	if p.Enabled && p.Binary == "" {
		return errors.New("enabled startup requires the installed Hub binary")
	}
	seen := map[string]bool{}
	for _, c := range p.Clients {
		if !endpointPattern.MatchString(c.Endpoint) || seen[c.Endpoint] {
			return errors.New("startup Client IDs must be unique local endpoint IDs")
		}
		seen[c.Endpoint] = true
		if c.RequiresHost {
			if c.Command != "" || c.Cwd != "" || len(c.Args) > 0 || len(c.Environment) > 0 {
				return errors.New("host-only startup entries must not store a command or host environment")
			}
			continue
		}
		if !filepath.IsAbs(c.Command) || !filepath.IsAbs(c.Cwd) || len(c.Args) > 128 {
			return errors.New("standalone Client needs an absolute executable and working directory")
		}
		for _, v := range append([]string{c.Command, c.Cwd}, c.Args...) {
			if !validPlanValue(v) {
				return errors.New("invalid startup process argument")
			}
		}
		for _, arg := range c.Args {
			if sensitiveStartupArgument(arg) || urlContainsCredentials(arg) {
				return errors.New("startup arguments must not store credentials or native host IPC settings")
			}
		}
		environmentKeys := map[string]bool{}
		for k, v := range c.Environment {
			// Host IPC paths, API keys and arbitrary loader settings must not be
			// captured from the current assistant into a persistent login item.
			folded := strings.ToUpper(k)
			if !allowedEnvironment(k) || !validPlanValue(v) || environmentKeys[folded] {
				return errors.New("startup environment permits only explicit OS, locale and proxy settings; host IPC and credentials are not captured")
			}
			environmentKeys[folded] = true
			if folded == "HTTP_PROXY" || folded == "HTTPS_PROXY" || folded == "ALL_PROXY" {
				if err := validateStartupProxy(v); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func validatePlanDirectory(dir string) error {
	if !filepath.IsAbs(dir) || !validPlanValue(dir) {
		return errors.New("startup data directory must be an absolute path")
	}
	return nil
}

func validPlanValue(value string) bool {
	return len(value) <= 32768 && utf8.ValidString(value) && !strings.ContainsRune(value, 0)
}

func sensitiveStartupArgument(arg string) bool {
	flag, _, _ := strings.Cut(strings.ToLower(arg), "=")
	switch flag {
	case "--api-key", "--apikey", "--access-token", "--auth-token", "--token", "--password", "--secret", "--client-secret", "--authorization", "--host-ipc", "--ipc-socket", "--ipc-path":
		return true
	}
	return false
}

func urlContainsCredentials(value string) bool {
	if strings.HasPrefix(value, "-") {
		if _, argumentValue, ok := strings.Cut(value, "="); ok {
			value = argumentValue
		}
	}
	parsed, err := url.Parse(value)
	return err == nil && parsed.Scheme != "" && parsed.User != nil
}

func validateStartupProxy(value string) error {
	if value == "" {
		return nil
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" || parsed.User != nil || (parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("startup proxy must be an explicit proxy URL without credentials, path, query, or fragment")
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https", "socks5", "socks5h":
		return nil
	}
	return errors.New("startup proxy URL scheme must be http, https, socks5, or socks5h")
}

func allowedEnvironment(k string) bool {
	switch k {
	case "PATH", "HOME", "USER", "USERNAME", "LOGNAME", "SHELL", "TMPDIR", "TMP", "TEMP", "LANG", "LC_ALL", "LC_CTYPE", "TZ", "SYSTEMROOT", "SystemRoot", "WINDIR", "USERPROFILE", "HOMEDRIVE", "HOMEPATH", "APPDATA", "LOCALAPPDATA", "PROGRAMFILES", "PROGRAMFILES(X86)", "COMMONPROGRAMFILES", "SYSTEMDRIVE", "PATHEXT", "HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY", "http_proxy", "https_proxy", "all_proxy", "no_proxy":
		return true
	}
	return false
}

func Save(dir string, p Plan) error {
	if err := Validate(p); err != nil {
		return err
	}
	return withPlanLock(dir, func() error {
		return writeJSON(filepath.Join(dir, "startup.json"), p)
	})
}

func withPlanLock(dir string, change func() error) error {
	if err := validatePlanDirectory(dir); err != nil {
		return err
	}
	planMu.Lock()
	defer planMu.Unlock()
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	lock, err := acquireRuntimeNamedLock(dir, planLockName)
	if errors.Is(err, ErrAlreadyRunning) {
		return fmt.Errorf("another process is changing startup configuration; retry: %w", err)
	}
	if err != nil {
		return fmt.Errorf("lock startup configuration: %w", err)
	}
	defer lock.Close()
	return change()
}

// Register records an approved external Client command, without starting it.
func Register(dir string, client Client) error {
	if err := Validate(Plan{Version: 1, Clients: []Client{client}}); err != nil {
		return err
	}
	return withPlanLock(dir, func() error {
		p, err := Load(dir)
		if err != nil {
			return err
		}
		for i, c := range p.Clients {
			if c.Endpoint == client.Endpoint {
				client.Enabled = c.Enabled
				p.Clients[i] = client
				return writeJSON(filepath.Join(dir, "startup.json"), p)
			}
		}
		client.Enabled = false
		p.Clients = append(p.Clients, client)
		if err := Validate(p); err != nil {
			return err
		}
		return writeJSON(filepath.Join(dir, "startup.json"), p)
	})
}

func Remove(dir, endpoint string) error {
	if !endpointPattern.MatchString(endpoint) {
		return errors.New("invalid startup Client ID")
	}
	return withPlanLock(dir, func() error {
		p, err := Load(dir)
		if err != nil {
			return err
		}
		out := []Client{}
		found := false
		for _, c := range p.Clients {
			if c.Endpoint == endpoint {
				found = true
			} else {
				out = append(out, c)
			}
		}
		if !found {
			return errors.New("startup Client is not registered")
		}
		p.Clients = out
		return writeJSON(filepath.Join(dir, "startup.json"), p)
	})
}

// Configure changes future login startup only. Running processes are untouched.
// Only a local CLI may register executable paths; the web UI selects known IDs.
func Configure(dir, binary string, enabled bool, selected []string) error {
	return configureWithPlatform(dir, binary, enabled, selected, planPlatformOperations{enable: PlatformEnable, disable: PlatformDisable})
}

type planPlatformOperations struct {
	enable  func(Registration) error
	disable func(Registration) error
}

func configureWithPlatform(dir, binary string, enabled bool, selected []string, platform planPlatformOperations) error {
	return withPlanLock(dir, func() error {
		return configureLocked(dir, binary, enabled, selected, platform)
	})
}

func configureLocked(dir, binary string, enabled bool, selected []string, platform planPlatformOperations) error {
	p, err := Load(dir)
	if err != nil {
		return err
	}
	old := p
	p.Clients = append([]Client(nil), p.Clients...)
	if binary != "" {
		p.Binary = binary
	}
	p.Enabled = enabled
	if selected != nil {
		wanted := map[string]bool{}
		for _, id := range selected {
			if wanted[id] {
				return errors.New("duplicate startup Client")
			}
			wanted[id] = true
		}
		for i := range p.Clients {
			p.Clients[i].Enabled = wanted[p.Clients[i].Endpoint]
			delete(wanted, p.Clients[i].Endpoint)
		}
		if len(wanted) > 0 {
			return errors.New("only registered startup Clients can be selected")
		}
	}
	if err := Validate(p); err != nil {
		return err
	}
	staged, err := stageJSON(filepath.Join(dir, "startup.json"), p)
	if err != nil {
		return err
	}
	defer staged.cleanup()
	reg := RegistrationFor(dir, p.Binary)
	if enabled {
		err = platform.enable(reg)
	} else {
		err = platform.disable(reg)
	}
	if err != nil {
		return err
	}
	if err = staged.commit(); err != nil {
		var rollbackErr error
		if old.Enabled {
			rollbackErr = platform.enable(RegistrationFor(dir, old.Binary))
		} else {
			rollbackErr = platform.disable(reg)
		}
		if rollbackErr != nil {
			return errors.Join(err, fmt.Errorf("restore the previous login startup definition: %w", rollbackErr))
		}
		return err
	}
	return nil
}
