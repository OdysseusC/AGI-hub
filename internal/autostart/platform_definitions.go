package autostart

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"unicode/utf16"
)

type inspectedDefinition struct {
	matches bool
	enabled bool
}

type platformXMLNode struct {
	XMLName  xml.Name
	Text     string            `xml:",chardata"`
	Comment  string            `xml:",comment"`
	Children []platformXMLNode `xml:",any"`
}

func launchAgentDefinition(r Registration) ([]byte, error) {
	var out bytes.Buffer
	out.WriteString(xml.Header)
	out.WriteString("<!DOCTYPE plist PUBLIC \"-//Apple//DTD PLIST 1.0//EN\" \"http://www.apple.com/DTDs/PropertyList-1.0.dtd\">\n")
	out.WriteString("<plist version=\"1.0\"><dict><!-- " + platformOwner + " -->\n")
	writePlistString := func(key, value string) {
		out.WriteString("<key>" + key + "</key><string>")
		xml.EscapeText(&out, []byte(value))
		out.WriteString("</string>\n")
	}
	writePlistString("Label", r.ID)
	out.WriteString("<key>ProgramArguments</key><array>")
	for _, arg := range []string{r.Binary, "autostart", "run", "--data", r.DataDir} {
		out.WriteString("<string>")
		xml.EscapeText(&out, []byte(arg))
		out.WriteString("</string>")
	}
	out.WriteString("</array>\n<key>RunAtLoad</key><true/>\n")
	out.WriteString("<key>Umask</key><integer>63</integer>\n")
	writePlistString("StandardOutPath", filepath.Join(r.LogDir, "runner.stdout.log"))
	writePlistString("StandardErrorPath", filepath.Join(r.LogDir, "runner.stderr.log"))
	out.WriteString("</dict></plist>\n")
	return out.Bytes(), nil
}

func (p platformController) inspectDefinition(r Registration, definition []byte) (inspectedDefinition, error) {
	if p.osName == "windows" {
		return p.inspectScheduledTask(r, definition)
	}
	return inspectLaunchAgent(r, definition)
}

func inspectLaunchAgent(r Registration, definition []byte) (inspectedDefinition, error) {
	var root platformXMLNode
	if err := decodePlatformXML(definition, &root); err != nil {
		return inspectedDefinition{}, fmt.Errorf("decode LaunchAgent XML: %w", err)
	}
	if root.XMLName.Local != "plist" || len(root.Children) != 1 || root.Children[0].XMLName.Local != "dict" {
		return inspectedDefinition{}, errors.New("LaunchAgent does not contain one plist dictionary")
	}
	dict := root.Children[0]
	if strings.TrimSpace(dict.Comment) != platformOwner {
		return inspectedDefinition{}, errors.New("LaunchAgent ownership marker is absent")
	}
	values := make(map[string]platformXMLNode)
	if len(dict.Children)%2 != 0 {
		return inspectedDefinition{}, errors.New("LaunchAgent dictionary is malformed")
	}
	for i := 0; i < len(dict.Children); i += 2 {
		key, value := dict.Children[i], dict.Children[i+1]
		if key.XMLName.Local != "key" || len(key.Children) != 0 {
			return inspectedDefinition{}, errors.New("LaunchAgent dictionary key is malformed")
		}
		if _, duplicate := values[key.Text]; duplicate {
			return inspectedDefinition{}, errors.New("LaunchAgent dictionary contains a duplicate key")
		}
		values[key.Text] = value
	}
	if len(values) != 6 || !plistStringEquals(values["Label"], r.ID) {
		return inspectedDefinition{}, errors.New("LaunchAgent label or properties do not belong to this registration")
	}
	argsNode := values["ProgramArguments"]
	if argsNode.XMLName.Local != "array" || len(argsNode.Children) != 5 {
		return inspectedDefinition{}, errors.New("LaunchAgent runner arguments are invalid")
	}
	args := make([]string, 5)
	for i, node := range argsNode.Children {
		if node.XMLName.Local != "string" || len(node.Children) != 0 {
			return inspectedDefinition{}, errors.New("LaunchAgent runner arguments must be strings")
		}
		args[i] = node.Text
	}
	if args[1] != "autostart" || args[2] != "run" || args[3] != "--data" || !filepath.IsAbs(args[0]) || !filepath.IsAbs(args[4]) {
		return inspectedDefinition{}, errors.New("LaunchAgent is not an AGI Hub login runner")
	}
	stdout, stderr := values["StandardOutPath"], values["StandardErrorPath"]
	if stdout.XMLName.Local != "string" || stderr.XMLName.Local != "string" || len(stdout.Children) != 0 || len(stderr.Children) != 0 || !filepath.IsAbs(stdout.Text) || filepath.Dir(stdout.Text) != filepath.Dir(stderr.Text) || filepath.Base(stdout.Text) != "runner.stdout.log" || filepath.Base(stderr.Text) != "runner.stderr.log" {
		return inspectedDefinition{}, errors.New("LaunchAgent log paths are invalid")
	}
	runAtLoad := values["RunAtLoad"]
	if (runAtLoad.XMLName.Local != "true" && runAtLoad.XMLName.Local != "false") || len(runAtLoad.Children) != 0 {
		return inspectedDefinition{}, errors.New("LaunchAgent login trigger is invalid")
	}
	umask := values["Umask"]
	if umask.XMLName.Local != "integer" || strings.TrimSpace(umask.Text) != "63" || len(umask.Children) != 0 {
		return inspectedDefinition{}, errors.New("LaunchAgent log permissions must use a private umask")
	}
	return inspectedDefinition{
		matches: args[0] == r.Binary && args[4] == r.DataDir && stdout.Text == filepath.Join(r.LogDir, "runner.stdout.log") && stderr.Text == filepath.Join(r.LogDir, "runner.stderr.log"),
		enabled: runAtLoad.XMLName.Local == "true",
	}, nil
}

func plistStringEquals(node platformXMLNode, value string) bool {
	return node.XMLName.Local == "string" && len(node.Children) == 0 && node.Text == value
}

const taskXMLNamespace = "http://schemas.microsoft.com/windows/2004/02/mit/task"

type scheduledTaskXML struct {
	XMLName          xml.Name `xml:"Task"`
	Version          string   `xml:"version,attr"`
	XMLNS            string   `xml:"xmlns,attr"`
	RegistrationInfo struct {
		Description string `xml:"Description"`
		URI         string `xml:"URI"`
	} `xml:"RegistrationInfo"`
	Triggers struct {
		Items []scheduledLogonTrigger `xml:"LogonTrigger"`
		Other []platformXMLNode       `xml:",any"`
	} `xml:"Triggers"`
	Principals struct {
		Items []scheduledPrincipal `xml:"Principal"`
		Other []platformXMLNode    `xml:",any"`
	} `xml:"Principals"`
	Settings struct {
		MultipleInstancesPolicy    string `xml:"MultipleInstancesPolicy"`
		DisallowStartIfOnBatteries bool   `xml:"DisallowStartIfOnBatteries"`
		StopIfGoingOnBatteries     bool   `xml:"StopIfGoingOnBatteries"`
		AllowHardTerminate         bool   `xml:"AllowHardTerminate"`
		StartWhenAvailable         bool   `xml:"StartWhenAvailable"`
		Enabled                    bool   `xml:"Enabled"`
		ExecutionTimeLimit         string `xml:"ExecutionTimeLimit"`
	} `xml:"Settings"`
	Actions struct {
		Context string `xml:"Context,attr"`
		Items   []struct {
			Command   string            `xml:"Command"`
			Arguments string            `xml:"Arguments"`
			Other     []platformXMLNode `xml:",any"`
		} `xml:"Exec"`
		Other []platformXMLNode `xml:",any"`
	} `xml:"Actions"`
}

type scheduledLogonTrigger struct {
	Enabled bool   `xml:"Enabled"`
	UserID  string `xml:"UserId"`
}

type scheduledPrincipal struct {
	ID        string `xml:"id,attr"`
	UserID    string `xml:"UserId"`
	LogonType string `xml:"LogonType"`
	RunLevel  string `xml:"RunLevel"`
}

func taskDescription(id string) string {
	return "AGI Hub optional login startup; " + platformOwner + "; ID=" + id
}

func scheduledTaskDefinition(r Registration, username string) ([]byte, error) {
	task := scheduledTaskXML{Version: "1.2", XMLNS: taskXMLNamespace}
	task.RegistrationInfo.Description = taskDescription(r.ID)
	task.RegistrationInfo.URI = `\` + r.ID
	task.Triggers.Items = []scheduledLogonTrigger{{Enabled: true, UserID: username}}
	task.Principals.Items = []scheduledPrincipal{{ID: "CurrentUser", UserID: username, LogonType: "InteractiveToken", RunLevel: "LeastPrivilege"}}
	task.Settings.MultipleInstancesPolicy = "IgnoreNew"
	task.Settings.Enabled = true
	task.Settings.ExecutionTimeLimit = "PT0S"
	task.Actions.Context = "CurrentUser"
	task.Actions.Items = append(task.Actions.Items, struct {
		Command   string            `xml:"Command"`
		Arguments string            `xml:"Arguments"`
		Other     []platformXMLNode `xml:",any"`
	}{Command: r.Binary, Arguments: windowsCommandLine([]string{"autostart", "run", "--data", r.DataDir})})
	definition, err := xml.MarshalIndent(task, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode login scheduled task XML: %w", err)
	}
	return append([]byte(xml.Header), append(definition, '\n')...), nil
}

func (p platformController) inspectScheduledTask(r Registration, definition []byte) (inspectedDefinition, error) {
	var root platformXMLNode
	if err := decodePlatformXML(definition, &root); err != nil {
		return inspectedDefinition{}, fmt.Errorf("decode scheduled task XML: %w", err)
	}
	sections := make(map[string]bool)
	for _, section := range root.Children {
		switch section.XMLName.Local {
		case "RegistrationInfo", "Triggers", "Principals", "Settings", "Actions":
		default:
			return inspectedDefinition{}, errors.New("scheduled task contains an unexpected section")
		}
		if sections[section.XMLName.Local] {
			return inspectedDefinition{}, errors.New("scheduled task contains duplicate sections")
		}
		sections[section.XMLName.Local] = true
	}
	if len(sections) != 5 || containsNativeRetry(root) {
		return inspectedDefinition{}, errors.New("scheduled task is missing required sections or contains native repetition/restart settings")
	}
	var task scheduledTaskXML
	if err := decodePlatformXML(definition, &task); err != nil {
		return inspectedDefinition{}, fmt.Errorf("decode scheduled task XML: %w", err)
	}
	if task.XMLName.Space != taskXMLNamespace || task.RegistrationInfo.Description != taskDescription(r.ID) || task.RegistrationInfo.URI != `\`+r.ID {
		return inspectedDefinition{}, errors.New("scheduled task ownership marker or URI is invalid")
	}
	if len(task.Principals.Items) != 1 || len(task.Principals.Other) != 0 || len(task.Triggers.Items) != 1 || len(task.Triggers.Other) != 0 || len(task.Actions.Items) != 1 || len(task.Actions.Other) != 0 || len(task.Actions.Items[0].Other) != 0 {
		return inspectedDefinition{}, errors.New("scheduled task must contain one current-user login trigger and one runner action")
	}
	username, err := p.currentUser()
	if err != nil {
		return inspectedDefinition{}, fmt.Errorf("identify the current Windows user: %w", err)
	}
	principal := task.Principals.Items[0]
	trigger := task.Triggers.Items[0]
	if username == "" || principal.UserID != username || trigger.UserID != username || principal.LogonType != "InteractiveToken" || principal.RunLevel != "LeastPrivilege" || task.Actions.Context != principal.ID || task.Settings.MultipleInstancesPolicy != "IgnoreNew" || task.Settings.ExecutionTimeLimit != "PT0S" || task.Settings.StartWhenAvailable {
		return inspectedDefinition{}, errors.New("scheduled task is not a least-privilege current-user login runner")
	}
	action := task.Actions.Items[0]
	args, err := splitWindowsCommandLine(action.Arguments)
	if err != nil || len(args) != 4 || args[0] != "autostart" || args[1] != "run" || args[2] != "--data" || !platformAbsolutePath("windows", args[3]) || !platformAbsolutePath("windows", action.Command) {
		return inspectedDefinition{}, errors.New("scheduled task runner arguments are invalid")
	}
	return inspectedDefinition{matches: action.Command == r.Binary && args[3] == r.DataDir, enabled: trigger.Enabled && task.Settings.Enabled}, nil
}

func decodePlatformXML(definition []byte, value any) error {
	decoder := xml.NewDecoder(bytes.NewReader(definition))
	if err := decoder.Decode(value); err != nil {
		return err
	}
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if whitespace, ok := token.(xml.CharData); ok && len(bytes.TrimSpace(whitespace)) == 0 {
			continue
		}
		return errors.New("startup definition must contain exactly one XML document")
	}
}

func containsNativeRetry(node platformXMLNode) bool {
	if node.XMLName.Local == "RestartOnFailure" || node.XMLName.Local == "Repetition" {
		return true
	}
	for _, child := range node.Children {
		if containsNativeRetry(child) {
			return true
		}
	}
	return false
}

// windowsCommandLine uses the CommandLineToArgvW/CRT escaping rules. Every
// backslash preceding a quote is doubled, and a quoted final backslash is
// doubled as well. Passing the executable separately to Task Scheduler avoids
// executable-name ambiguities.
func windowsCommandLine(args []string) string {
	quoted := make([]string, len(args))
	for i, arg := range args {
		quoted[i] = windowsQuoteArg(arg)
	}
	return strings.Join(quoted, " ")
}

func windowsQuoteArg(arg string) string {
	if arg != "" && !strings.ContainsAny(arg, " \t\n\v\"") {
		return arg
	}
	var out strings.Builder
	out.WriteByte('"')
	slashes := 0
	for _, ch := range arg {
		if ch == '\\' {
			slashes++
			continue
		}
		if ch == '"' {
			out.WriteString(strings.Repeat(`\`, slashes*2+1))
		} else {
			out.WriteString(strings.Repeat(`\`, slashes))
		}
		out.WriteRune(ch)
		slashes = 0
	}
	out.WriteString(strings.Repeat(`\`, slashes*2))
	out.WriteByte('"')
	return out.String()
}

func splitWindowsCommandLine(commandLine string) ([]string, error) {
	var args []string
	for i := 0; i < len(commandLine); {
		for i < len(commandLine) && (commandLine[i] == ' ' || commandLine[i] == '\t') {
			i++
		}
		if i == len(commandLine) {
			break
		}
		var arg strings.Builder
		quoted := false
		for i < len(commandLine) {
			if !quoted && (commandLine[i] == ' ' || commandLine[i] == '\t') {
				break
			}
			slashes := 0
			for i < len(commandLine) && commandLine[i] == '\\' {
				slashes++
				i++
			}
			if i < len(commandLine) && commandLine[i] == '"' {
				arg.WriteString(strings.Repeat(`\`, slashes/2))
				if slashes%2 != 0 {
					arg.WriteByte('"')
				} else if quoted && i+1 < len(commandLine) && commandLine[i+1] == '"' {
					arg.WriteByte('"')
					i++
				} else {
					quoted = !quoted
				}
				i++
				continue
			}
			arg.WriteString(strings.Repeat(`\`, slashes))
			if i < len(commandLine) {
				if !quoted && (commandLine[i] == ' ' || commandLine[i] == '\t') {
					break
				}
				arg.WriteByte(commandLine[i])
				i++
			}
		}
		if quoted {
			return nil, errors.New("unclosed quote in Windows command line")
		}
		args = append(args, arg.String())
	}
	return args, nil
}

// Query through the native Task Scheduler COM API so missing tasks can be
// distinguished by HRESULT rather than localized schtasks error messages.
// The fixed, validated ID is the only interpolated value, and the encoded
// script never reads or stores a password. Use .NET methods rather than cmdlets:
// discovering New-Object can scan ambient modules when the login runner omits
// PowerShell's module-analysis cache environment setting.
func scheduledTaskQuery(id string) string {
	script := `$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = [System.Text.Encoding]::UTF8
$stage = 'create_service'
[Console]::Error.WriteLine('[agihub-startup-stage:' + $stage + ']')
try {
  $service = [Activator]::CreateInstance([type]::GetTypeFromProgID('Schedule.Service', $true))
  $stage = 'connect'
  [Console]::Error.WriteLine('[agihub-startup-stage:' + $stage + ']')
  $service.Connect()
  $stage = 'get_folder'
  [Console]::Error.WriteLine('[agihub-startup-stage:' + $stage + ']')
  $folder = $service.GetFolder('\')
  $stage = 'get_task'
  [Console]::Error.WriteLine('[agihub-startup-stage:' + $stage + ']')
  $task = $folder.GetTask('` + id + `')
  $stage = 'read_xml'
  [Console]::Error.WriteLine('[agihub-startup-stage:' + $stage + ']')
  $document = [System.Xml.XmlDocument]::new()
  $document.LoadXml($task.Xml)
  if ($document.FirstChild -is [System.Xml.XmlDeclaration]) { $document.FirstChild.Encoding = 'UTF-8' }
  [Console]::Out.Write($document.OuterXml)
} catch {
  $codes = @()
  $failure = $_.Exception
  while ($null -ne $failure) {
    if ($failure.HResult -eq -2147024894) { exit 3 }
    $codes += $failure.HResult
    $failure = $failure.InnerException
  }
  [Console]::Error.Write('task-scheduler query failed; stage=' + $stage + '; hresults=' + ($codes -join ','))
  exit 1
}`
	units := utf16.Encode([]rune(script))
	encoded := make([]byte, len(units)*2)
	for i, unit := range units {
		binary.LittleEndian.PutUint16(encoded[i*2:], unit)
	}
	return base64.StdEncoding.EncodeToString(encoded)
}
