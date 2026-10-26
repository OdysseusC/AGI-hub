package autostart

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/xml"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"unicode/utf16"
)

func macPlatformForTest(t *testing.T) (platformController, Registration) {
	t.Helper()
	home := t.TempDir()
	root := t.TempDir()
	return platformController{
			osName:  "darwin",
			homeDir: func() (string, error) { return home, nil },
			run: func(name string, args ...string) ([]byte, error) {
				t.Fatalf("unexpected native command: %s %v", name, args)
				return nil, nil
			},
		}, Registration{
			ID:      "com.agihub.startup.0123456789abcdef",
			Binary:  filepath.Join(root, "AGI Hub & 测试", "agihub"),
			DataDir: filepath.Join(root, "data & <private>"),
			LogDir:  filepath.Join(root, "runner logs"),
		}
}

func TestPlatformLaunchAgentLifecycleIsDefinitionOnly(t *testing.T) {
	p, r := macPlatformForTest(t)
	state, err := p.status(r)
	if err != nil || !state.Supported || state.Installed || state.Enabled {
		t.Fatalf("absent status = %+v, %v", state, err)
	}
	if err := p.enable(r); err != nil {
		t.Fatal(err)
	}
	path, err := p.launchAgentPath(r)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
		t.Fatalf("definition permissions = %o", info.Mode().Perm())
	}
	definition, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(definition, []byte("&amp;")) || !bytes.Contains(definition, []byte("&lt;private&gt;")) || !bytes.Contains(definition, []byte("<key>Umask</key><integer>63</integer>")) || bytes.Contains(definition, []byte("KeepAlive")) || bytes.Contains(definition, []byte("EnvironmentVariables")) {
		t.Fatalf("unexpected LaunchAgent definition: %s", definition)
	}
	state, err = p.status(r)
	if err != nil || !state.Installed || !state.Enabled || state.Mechanism != "launchd" {
		t.Fatalf("enabled status = %+v, %v", state, err)
	}
	r.Binary = filepath.Join(filepath.Dir(r.Binary), "updated agihub")
	state, err = p.status(r)
	if err != nil || !state.Installed || state.Enabled || !strings.Contains(state.Detail, "differs") {
		t.Fatalf("changed configuration status = %+v, %v", state, err)
	}
	if err := p.enable(r); err != nil {
		t.Fatalf("update owned definition: %v", err)
	}
	state, err = p.status(r)
	if err != nil || !state.Enabled {
		t.Fatalf("updated status = %+v, %v", state, err)
	}
	if err := p.disable(r); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("definition remains after disable: %v", err)
	}
	if err := p.disable(r); err != nil {
		t.Fatalf("idempotent disable: %v", err)
	}
}

func TestPlatformLaunchAgentRefusesForeignAndSymlinkDefinitions(t *testing.T) {
	for _, kind := range []string{"foreign", "symlink", "changed action"} {
		t.Run(kind, func(t *testing.T) {
			p, r := macPlatformForTest(t)
			path, _ := p.launchAgentPath(r)
			if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				t.Fatal(err)
			}
			original := []byte("<plist><dict><key>Label</key><string>foreign</string></dict></plist>")
			if kind == "changed action" {
				original, _ = launchAgentDefinition(r)
				original = bytes.Replace(original, []byte("<string>autostart</string>"), []byte("<string>foreign</string>"), 1)
			}
			if kind == "symlink" {
				target := filepath.Join(t.TempDir(), "foreign.plist")
				if err := os.WriteFile(target, original, 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, path); err != nil {
					t.Skipf("symlinks unavailable: %v", err)
				}
			} else if err := os.WriteFile(path, original, 0600); err != nil {
				t.Fatal(err)
			}
			if err := p.enable(r); err == nil {
				t.Fatal("enable accepted an unowned definition")
			}
			if err := p.disable(r); err == nil {
				t.Fatal("disable accepted an unowned definition")
			}
			actual, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(actual, original) {
				t.Fatalf("unowned definition changed: %q, %v", actual, err)
			}
		})
	}
}

func TestPlatformFailedLaunchAgentUpdatePreservesExistingFile(t *testing.T) {
	p, r := macPlatformForTest(t)
	if err := p.enable(r); err != nil {
		t.Fatal(err)
	}
	path, _ := p.launchAgentPath(r)
	before, _ := os.ReadFile(path)
	badLogDir := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(badLogDir, []byte("occupied"), 0600); err != nil {
		t.Fatal(err)
	}
	r.LogDir = filepath.Join(badLogDir, "logs")
	if err := p.enable(r); err == nil {
		t.Fatal("expected log directory creation to fail")
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("failed update replaced previous definition: %v", err)
	}
}

type platformExitError int

func (e platformExitError) Error() string { return "native command failed" }
func (e platformExitError) ExitCode() int { return int(e) }

func TestPlatformQueryFailureRetainsCapabilityAndSafeStage(t *testing.T) {
	p := platformController{osName: "windows", run: func(string, ...string) ([]byte, error) {
		return []byte("[agihub-startup-stage:create_service]\n[agihub-startup-stage:connect]\n"), context.DeadlineExceeded
	}}
	state, err := p.status(windowsRegistrationForTest())
	var failure *PlatformQueryError
	if !state.Supported || state.Mechanism != "task-scheduler" || !errors.As(err, &failure) || failure.Code != "timeout" || failure.Stage != "connect" || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("native query failure lost safe capability or stage: %+v %v", state, err)
	}
	for _, output := range []string{"arbitrary private text", "[agihub-startup-stage:private-value]", "[agihub-startup-stage:connect"} {
		if stage := scheduledTaskFailureStage([]byte(output)); stage != "" {
			t.Fatalf("untrusted native output became a public stage: %q", stage)
		}
	}
}

func windowsRegistrationForTest() Registration {
	return Registration{
		ID:      "com.agihub.startup.0123456789abcdef",
		Binary:  `C:\Program Files\AGI Hub\agihub.exe`,
		DataDir: `C:\Users\测试用户\Hub data\`,
		LogDir:  `C:\Users\测试用户\Hub data\logs`,
	}
}

func TestPlatformScheduledTaskLifecycleIsCurrentUserDefinitionOnly(t *testing.T) {
	r := windowsRegistrationForTest()
	var definition []byte
	var calls [][]string
	p := platformController{
		osName:      "windows",
		currentUser: func() (string, error) { return "S-1-5-21-1234", nil },
		tempDir:     t.TempDir(),
	}
	p.run = func(name string, args ...string) ([]byte, error) {
		calls = append(calls, append([]string{name}, args...))
		if name == "powershell.exe" {
			if len(definition) == 0 {
				return nil, platformExitError(3)
			}
			return definition, nil
		}
		if name != "schtasks.exe" || len(args) == 0 {
			t.Fatalf("unexpected command: %s %v", name, args)
		}
		switch args[0] {
		case "/Create":
			if len(args) < 5 || args[1] != "/TN" || args[2] != r.ID || args[3] != "/XML" {
				t.Fatalf("unexpected create arguments: %v", args)
			}
			info, err := os.Stat(args[4])
			if err != nil {
				t.Fatalf("temporary definition is unavailable: %v", err)
			}
			if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
				t.Fatalf("temporary definition mode: %v, %v", info, err)
			}
			definition, err = os.ReadFile(args[4])
			return nil, err
		case "/Delete":
			definition = nil
			return nil, nil
		default:
			t.Fatalf("a registration command could start or stop a process: %v", args)
			return nil, nil
		}
	}
	state, err := p.status(r)
	if err != nil || !state.Supported || state.Installed || state.Enabled {
		t.Fatalf("absent task status: %+v, %v", state, err)
	}
	if err := p.enable(r); err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"<LogonType>InteractiveToken</LogonType>", "<RunLevel>LeastPrivilege</RunLevel>", "<MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>", "<ExecutionTimeLimit>PT0S</ExecutionTimeLimit>"} {
		if !bytes.Contains(definition, []byte(required)) {
			t.Fatalf("task is missing %q: %s", required, definition)
		}
	}
	if bytes.Contains(definition, []byte("Password")) || bytes.Contains(definition, []byte("RestartOnFailure")) || bytes.Contains(definition, []byte("Environment")) {
		t.Fatalf("task stores credentials/environment or configures native retries: %s", definition)
	}
	state, err = p.status(r)
	if err != nil || !state.Installed || !state.Enabled || state.Mechanism != "task-scheduler" {
		t.Fatalf("registered task status: %+v, %v", state, err)
	}
	r.Binary = `C:\Program Files\AGI Hub\updated agihub.exe`
	state, err = p.status(r)
	if err != nil || !state.Installed || state.Enabled {
		t.Fatalf("changed task status: %+v, %v", state, err)
	}
	if err := p.enable(r); err != nil {
		t.Fatal(err)
	}
	lastCreate := calls[len(calls)-1]
	if lastCreate[len(lastCreate)-1] != "/F" {
		t.Fatalf("owned task update must be explicit: %v", lastCreate)
	}
	if err := p.disable(r); err != nil {
		t.Fatal(err)
	}
	state, err = p.status(r)
	if err != nil || state.Installed || state.Enabled {
		t.Fatalf("removed task status: %+v, %v", state, err)
	}
	entries, err := os.ReadDir(p.tempDir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("temporary XML remains: %v, %v", entries, err)
	}
	for _, call := range calls {
		for _, arg := range call[1:] {
			if strings.EqualFold(arg, "/Run") || strings.EqualFold(arg, "/End") {
				t.Fatalf("registration changed current process state: %v", call)
			}
		}
	}
}

func TestPlatformScheduledTaskRefusesUnownedOrUnsafeDefinitions(t *testing.T) {
	r := windowsRegistrationForTest()
	definition, _ := scheduledTaskDefinition(r, "S-1-5-21-1234")
	for name, change := range map[string]func([]byte) []byte{
		"foreign owner":      func(b []byte) []byte { return bytes.ReplaceAll(b, []byte(platformOwner), []byte("foreign")) },
		"other current user": func(b []byte) []byte { return bytes.ReplaceAll(b, []byte("S-1-5-21-1234"), []byte("S-1-5-21-9999")) },
		"elevated principal": func(b []byte) []byte {
			return bytes.ReplaceAll(b, []byte("LeastPrivilege"), []byte("HighestAvailable"))
		},
		"parallel instances": func(b []byte) []byte { return bytes.ReplaceAll(b, []byte("IgnoreNew"), []byte("Parallel")) },
		"other action": func(b []byte) []byte {
			return bytes.ReplaceAll(b, []byte("autostart run --data"), []byte("foreign run --data"))
		},
		"native retries": func(b []byte) []byte {
			return bytes.Replace(b, []byte("</Settings>"), []byte("<RestartOnFailure><Interval>PT1M</Interval><Count>999</Count></RestartOnFailure></Settings>"), 1)
		},
		"repeated login trigger": func(b []byte) []byte {
			return bytes.Replace(b, []byte("<LogonTrigger>"), []byte("<LogonTrigger><Repetition><Interval>PT1M</Interval></Repetition>"), 1)
		},
	} {
		t.Run(name, func(t *testing.T) {
			mutated := change(definition)
			p := platformController{
				osName:      "windows",
				currentUser: func() (string, error) { return "S-1-5-21-1234", nil },
				run: func(name string, args ...string) ([]byte, error) {
					if name != "powershell.exe" {
						t.Fatalf("mutated an unowned task: %s %v", name, args)
					}
					return mutated, nil
				},
			}
			if err := p.enable(r); err == nil {
				t.Fatal("enable accepted an unowned/unsafe task")
			}
			if err := p.disable(r); err == nil {
				t.Fatal("disable accepted an unowned/unsafe task")
			}
		})
	}
}

func TestPlatformEmptyBinaryCanBeInspectedOrDisabledButNotEnabled(t *testing.T) {
	p, r := macPlatformForTest(t)
	r.Binary = ""
	if state, err := p.status(r); err != nil || state.Installed {
		t.Fatalf("unconfigured startup status = %+v, %v", state, err)
	}
	if err := p.disable(r); err != nil {
		t.Fatalf("disable unconfigured startup: %v", err)
	}
	if err := p.enable(r); err == nil {
		t.Fatal("enabled startup without a binary")
	}
}

func TestPlatformScheduledTaskQueryErrorsDoNotBecomeMissingTasks(t *testing.T) {
	r := windowsRegistrationForTest()
	p := platformController{
		osName: "windows",
		run: func(string, ...string) ([]byte, error) {
			return []byte("localized query failure"), platformExitError(1)
		},
	}
	if _, err := p.status(r); err == nil {
		t.Fatal("query failure reported a missing task")
	}
	if err := p.enable(r); err == nil {
		t.Fatal("query failure allowed registration")
	}
}

func TestPlatformWindowsCommandLineQuoting(t *testing.T) {
	cases := []struct{ input, want string }{
		{"", `""`},
		{"simple", "simple"},
		{`C:\path\`, `C:\path\`},
		{`C:\with space\`, `"C:\with space\\"`},
		{`a"b`, `"a\"b"`},
		{`a\"b`, `"a\\\"b"`},
		{`测试 & <user>`, `"测试 & <user>"`},
	}
	for _, tc := range cases {
		if got := windowsQuoteArg(tc.input); got != tc.want {
			t.Errorf("quote %q = %q, want %q", tc.input, got, tc.want)
		}
	}
	want := []string{"autostart", "run", "--data", `C:\用户 with spaces\a"quoted"\`, "", `\\server\share\`}
	got, err := splitWindowsCommandLine(windowsCommandLine(want))
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("command line round trip = %#v, %v; want %#v", got, err, want)
	}
}

func TestPlatformScheduledTaskDefinitionEscapesXML(t *testing.T) {
	r := windowsRegistrationForTest()
	r.DataDir = `C:\测试 & <private>\quoted "dir"\`
	r.Binary = `C:\Program Files\AGI & Hub\agihub.exe`
	definition, err := scheduledTaskDefinition(r, `DOMAIN\user & 测试`)
	if err != nil {
		t.Fatal(err)
	}
	var task scheduledTaskXML
	if err := xml.Unmarshal(definition, &task); err != nil {
		t.Fatal(err)
	}
	args, err := splitWindowsCommandLine(task.Actions.Items[0].Arguments)
	if err != nil || !reflect.DeepEqual(args, []string{"autostart", "run", "--data", r.DataDir}) || task.Actions.Items[0].Command != r.Binary || task.Principals.Items[0].UserID != `DOMAIN\user & 测试` {
		t.Fatalf("escaped task values changed: %+v %v", task, err)
	}
}

func TestPlatformTaskQueryUsesEncodedNativeXMLAndExplicitMissingExitCode(t *testing.T) {
	id := "com.agihub.startup.0123456789abcdef"
	encoded, err := base64.StdEncoding.DecodeString(scheduledTaskQuery(id))
	if err != nil {
		t.Fatal(err)
	}
	units := make([]uint16, len(encoded)/2)
	for i := range units {
		units[i] = binary.LittleEndian.Uint16(encoded[i*2:])
	}
	script := string(utf16.Decode(units))
	for _, required := range []string{"Schedule.Service", "GetTask('" + id + "')", "$task.Xml", "exit 3", "-2147024894"} {
		if !strings.Contains(script, required) {
			t.Fatalf("query script missing %q: %s", required, script)
		}
	}
	if strings.Contains(script, "New-Object") || strings.Contains(script, "Import-Module") {
		t.Fatal("native task query depends on ambient PowerShell cmdlet/module discovery")
	}
	if strings.Contains(script, "-2147216625") {
		t.Fatal("account-information errors must not be classified as a missing task")
	}
}

func TestPlatformUnsupportedDoesNotRunCommands(t *testing.T) {
	p := platformController{osName: "linux", run: func(string, ...string) ([]byte, error) {
		t.Fatal("unsupported platform ran a command")
		return nil, nil
	}}
	state, err := p.status(Registration{})
	if err != nil || state.Supported || state.Installed || state.Enabled || !strings.Contains(state.Detail, "unsupported on linux") {
		t.Fatalf("unsupported status = %+v, %v", state, err)
	}
	if err := p.enable(Registration{}); !errors.Is(err, ErrUnsupportedPlatform) {
		t.Fatalf("enable unsupported = %v", err)
	}
	if err := p.disable(Registration{}); !errors.Is(err, ErrUnsupportedPlatform) {
		t.Fatalf("disable unsupported = %v", err)
	}
}

func TestPlatformRejectsUnsafeRegistrationBeforeCommands(t *testing.T) {
	for _, mutate := range []func(*Registration){
		func(r *Registration) { r.ID = "../foreign" },
		func(r *Registration) { r.ID = "com.other.startup.id" },
		func(r *Registration) { r.Binary = "relative.exe" },
		func(r *Registration) { r.DataDir += "\x00" },
		func(r *Registration) { r.LogDir += "\x01" },
	} {
		r := windowsRegistrationForTest()
		mutate(&r)
		p := platformController{osName: "windows", run: func(string, ...string) ([]byte, error) {
			t.Fatal("invalid registration ran a command")
			return nil, nil
		}}
		if _, err := p.status(r); err == nil {
			t.Fatalf("accepted unsafe registration: %+v", r)
		}
		if err := p.enable(r); err == nil {
			t.Fatalf("enabled unsafe registration: %+v", r)
		}
		if err := p.disable(r); err == nil {
			t.Fatalf("disabled unsafe registration: %+v", r)
		}
	}
}
