package autostart

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"runtime"
	"time"
	"unicode/utf8"
)

// Registration identifies one user's optional login runner. It contains no
// credentials: the runner reads its ordinary configuration from DataDir.
type Registration struct {
	ID      string
	Binary  string
	DataDir string
	LogDir  string
}

// PlatformState reports the definition used at a future login. It does not
// report whether a runner, Hub, or Client is currently running.
type PlatformState struct {
	Supported bool   `json:"supported"`
	Installed bool   `json:"installed"`
	Enabled   bool   `json:"enabled"`
	Mechanism string `json:"mechanism"`
	Detail    string `json:"detail"`
}

var ErrUnsupportedPlatform = errors.New("optional login startup is unsupported on this platform")

const platformOwner = "agihub-autostart:v1"

var registrationIDPattern = regexp.MustCompile(`^com\.agihub\.startup\.[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}$`)

type platformController struct {
	osName      string
	homeDir     func() (string, error)
	currentUser func() (string, error)
	run         func(string, ...string) ([]byte, error)
	tempDir     string
}

func nativePlatformController() platformController {
	return platformController{
		osName:  runtime.GOOS,
		homeDir: os.UserHomeDir,
		currentUser: func() (string, error) {
			u, err := user.Current()
			if err != nil {
				return "", err
			}
			// Windows normalizes task principals to a SID. Using the SID
			// also avoids localized or renamed account-name comparisons.
			return u.Uid, nil
		},
		run: func(name string, args ...string) ([]byte, error) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, name, args...)
			command.WaitDelay = time.Second
			return command.CombinedOutput()
		},
	}
}

// PlatformStatus checks the actual installed definition, without registering,
// loading, starting, or stopping a process.
func PlatformStatus(r Registration) (PlatformState, error) {
	return nativePlatformController().status(r)
}

// PlatformEnable registers a runner for the next user login. It deliberately
// does not bootstrap a LaunchAgent or run a newly registered scheduled task.
func PlatformEnable(r Registration) error {
	return nativePlatformController().enable(r)
}

// PlatformDisable removes future login triggers. Removing a definition does
// not unload a LaunchAgent, end a task, or stop any currently running process.
func PlatformDisable(r Registration) error {
	return nativePlatformController().disable(r)
}

func (p platformController) state() PlatformState {
	switch p.osName {
	case "darwin":
		return PlatformState{Supported: true, Mechanism: "launchd"}
	case "windows":
		return PlatformState{Supported: true, Mechanism: "task-scheduler"}
	default:
		return PlatformState{Mechanism: "unsupported", Detail: fmt.Sprintf("optional login startup is unsupported on %s", p.osName)}
	}
}

func (p platformController) status(r Registration) (PlatformState, error) {
	state := p.state()
	if !state.Supported {
		return state, nil
	}
	if err := p.validate(r); err != nil {
		return state, err
	}
	definition, exists, err := p.readDefinition(r)
	state.Installed = exists
	if err != nil {
		return state, err
	}
	if !exists {
		state.Detail = "no login startup definition is installed"
		return state, nil
	}
	actual, err := p.inspectDefinition(r, definition)
	if err != nil {
		state.Detail = "the existing definition is not a valid AGI Hub login startup definition"
		return state, err
	}
	if !actual.matches {
		state.Detail = "the installed definition differs from the current binary, data directory, or log directory"
		return state, nil
	}
	state.Enabled = actual.enabled
	if actual.enabled {
		state.Detail = "configured for a future user login; current process state is separate"
	} else {
		state.Detail = "the installed login trigger is disabled"
	}
	return state, nil
}

func (p platformController) enable(r Registration) error {
	if !p.state().Supported {
		return fmt.Errorf("%w: %s", ErrUnsupportedPlatform, p.osName)
	}
	if err := p.validate(r); err != nil {
		return err
	}
	if r.Binary == "" {
		return errors.New("login startup binary must be an absolute path")
	}
	old, exists, err := p.readDefinition(r)
	if err != nil {
		return err
	}
	if exists {
		if _, err := p.inspectDefinition(r, old); err != nil {
			return fmt.Errorf("refusing to replace an unowned login startup definition: %w", err)
		}
	}
	if p.osName == "darwin" {
		definition, err := launchAgentDefinition(r)
		if err != nil {
			return err
		}
		path, err := p.launchAgentPath(r)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(r.LogDir, 0700); err != nil {
			return fmt.Errorf("create login runner log directory: %w", err)
		}
		return atomicPrivateDefinition(path, definition)
	}
	username, err := p.currentUser()
	if err != nil {
		return fmt.Errorf("identify the current Windows user: %w", err)
	}
	if username == "" {
		return errors.New("the current Windows user is empty")
	}
	definition, err := scheduledTaskDefinition(r, username)
	if err != nil {
		return err
	}
	path, cleanup, err := p.temporaryDefinition(definition)
	if err != nil {
		return err
	}
	defer cleanup()
	args := []string{"/Create", "/TN", r.ID, "/XML", path}
	if exists {
		args = append(args, "/F")
	}
	if out, err := p.run("schtasks.exe", args...); err != nil {
		return fmt.Errorf("register login scheduled task: %w%s", err, commandOutput(out))
	}
	return nil
}

func (p platformController) disable(r Registration) error {
	if !p.state().Supported {
		return fmt.Errorf("%w: %s", ErrUnsupportedPlatform, p.osName)
	}
	if err := p.validate(r); err != nil {
		return err
	}
	definition, exists, err := p.readDefinition(r)
	if err != nil || !exists {
		return err
	}
	if _, err := p.inspectDefinition(r, definition); err != nil {
		return fmt.Errorf("refusing to remove an unowned login startup definition: %w", err)
	}
	if p.osName == "darwin" {
		path, err := p.launchAgentPath(r)
		if err != nil {
			return err
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove login LaunchAgent definition: %w", err)
		}
		return nil
	}
	if out, err := p.run("schtasks.exe", "/Delete", "/TN", r.ID, "/F"); err != nil {
		return fmt.Errorf("remove login scheduled task: %w%s", err, commandOutput(out))
	}
	return nil
}

func (p platformController) validate(r Registration) error {
	if !registrationIDPattern.MatchString(r.ID) {
		return errors.New("login startup ID must use the com.agihub.startup namespace and contain only letters, digits, dots, underscores, or hyphens")
	}
	for _, value := range []struct{ name, path string }{{"binary", r.Binary}, {"data directory", r.DataDir}, {"log directory", r.LogDir}} {
		if value.name == "binary" && value.path == "" {
			continue
		}
		if !platformAbsolutePath(p.osName, value.path) || !validDefinitionText(value.path) {
			return fmt.Errorf("login startup %s must be an absolute path containing valid XML text", value.name)
		}
	}
	return nil
}

func validDefinitionText(value string) bool {
	if !utf8.ValidString(value) {
		return false
	}
	for _, ch := range value {
		if ch != '\t' && ch != '\n' && ch != '\r' && (ch < 0x20 || ch == 0xfffe || ch == 0xffff) {
			return false
		}
	}
	return true
}

func platformAbsolutePath(osName, path string) bool {
	if osName != "windows" {
		return filepath.IsAbs(path)
	}
	if len(path) >= 3 && ((path[0] >= 'A' && path[0] <= 'Z') || (path[0] >= 'a' && path[0] <= 'z')) && path[1] == ':' && (path[2] == '\\' || path[2] == '/') {
		return true
	}
	return len(path) > 2 && path[0] == '\\' && path[1] == '\\'
}

func (p platformController) launchAgentPath(r Registration) (string, error) {
	home, err := p.homeDir()
	if err != nil {
		return "", fmt.Errorf("find the current user's home directory: %w", err)
	}
	if home == "" {
		return "", errors.New("the current user's home directory is empty")
	}
	return filepath.Join(home, "Library", "LaunchAgents", r.ID+".plist"), nil
}

func (p platformController) readDefinition(r Registration) ([]byte, bool, error) {
	if p.osName == "windows" {
		out, err := p.run("powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive", "-EncodedCommand", scheduledTaskQuery(r.ID))
		if err != nil {
			var exitErr interface{ ExitCode() int }
			if errors.As(err, &exitErr) && exitErr.ExitCode() == 3 {
				return nil, false, nil
			}
			return nil, false, fmt.Errorf("query login scheduled task: %w%s", err, commandOutput(out))
		}
		return bytes.TrimPrefix(out, []byte{0xef, 0xbb, 0xbf}), true, nil
	}
	path, err := p.launchAgentPath(r)
	if err != nil {
		return nil, false, err
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("inspect login LaunchAgent definition: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, true, errors.New("login LaunchAgent definition is not a regular file")
	}
	definition, err := os.ReadFile(path)
	if err != nil {
		return nil, true, fmt.Errorf("read login LaunchAgent definition: %w", err)
	}
	return definition, true, nil
}

func atomicPrivateDefinition(path string, definition []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create login definition directory: %w", err)
	}
	f, err := os.CreateTemp(dir, ".agihub-autostart-*")
	if err != nil {
		return fmt.Errorf("stage login definition: %w", err)
	}
	defer os.Remove(f.Name())
	if err := f.Chmod(0600); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(definition); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(f.Name(), path); err != nil {
		return fmt.Errorf("install login definition: %w", err)
	}
	return nil
}

func (p platformController) temporaryDefinition(definition []byte) (string, func(), error) {
	f, err := os.CreateTemp(p.tempDir, ".agihub-autostart-*.xml")
	if err != nil {
		return "", nil, fmt.Errorf("stage login scheduled task: %w", err)
	}
	cleanup := func() { os.Remove(f.Name()) }
	if err := f.Chmod(0600); err != nil {
		f.Close()
		cleanup()
		return "", nil, err
	}
	if _, err := f.Write(definition); err != nil {
		f.Close()
		cleanup()
		return "", nil, err
	}
	if err := f.Close(); err != nil {
		cleanup()
		return "", nil, err
	}
	return f.Name(), cleanup, nil
}

func commandOutput(out []byte) string {
	text := string(bytes.TrimSpace(out))
	if text == "" {
		return ""
	}
	return ": " + text
}
