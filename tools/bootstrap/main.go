// Portable, offline first installation. It never starts a model or a Client.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"

	"agihub/internal/app"
)

type packageFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Mode   uint32 `json:"mode"`
}

type packageManifest struct {
	Version  int           `json:"version"`
	Platform string        `json:"platform"`
	Files    []packageFile `json:"files"`
}

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "安装停止：", err)
		os.Exit(1)
	}
}

func run(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("agi-hub-setup", flag.ContinueOnError)
	fs.SetOutput(out)
	host := fs.String("host", "", "explicitly selected host: codex or workbuddy")
	endpoint := fs.String("endpoint", "", "configured endpoint ID (defaults to host)")
	data := fs.String("data", app.DefaultDataDir(), "private Hub data directory, outside this package")
	source := fs.String("package-dir", "", "extracted package directory (defaults to this executable's directory)")
	skillsDir := fs.String("skills-dir", "", "verified host skill directory, if different from its default")
	reportBinary := fs.String("report-binary", "", "existing endpoint-scoped report helper, if configured")
	clientConfig := fs.String("client-config", "", "existing endpoint-scoped Client JSON for a new fixed helper")
	workspace := fs.String("workspace", "", "explicitly authorized absolute workspace for the fixed helper")
	verify := fs.Bool("verify-only", false, "verify the local package without installing or starting anything")
	fs.Usage = func() {
		fmt.Fprintln(out, "AGI Hub 本机引导安装器\n先读 START-HERE.md；不会调用模型、登录账号、启用会话或启动后台程序。\n用法：agi-hub-setup --host codex [--data 私有目录]\n只检查安装包：agi-hub-setup --verify-only")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	if !*verify && *host == "" {
		fs.Usage()
		return nil
	}
	if *host != "" && *host != "codex" && *host != "workbuddy" {
		return errors.New("第一版引导安装只接受明确选定的 codex 或 workbuddy")
	}
	if *source == "" {
		executable, err := os.Executable()
		if err != nil {
			return err
		}
		*source = filepath.Dir(executable)
	}
	var err error
	*source, err = filepath.Abs(*source)
	if err != nil {
		return err
	}
	manifest, manifestBytes, err := readAndVerify(*source)
	if err != nil {
		return err
	}
	if *verify {
		fmt.Fprintf(out, "安装包校验通过：%s，%d 个公开文件；未安装、未调用助手。\n", manifest.Platform, len(manifest.Files))
		return nil
	}
	*data, err = filepath.Abs(*data)
	if err != nil {
		return err
	}
	if within(*source, *data) {
		return errors.New("私有数据目录不能放在解压安装包里面")
	}
	if err := noSymlinks(*data); err != nil {
		return err
	}
	if err := os.MkdirAll(*data, 0700); err != nil {
		return err
	}
	digest := sha256.Sum256(manifestBytes)
	installDir := filepath.Join(*data, "runtimes", "portable-"+manifest.Platform+"-"+hex.EncodeToString(digest[:6]))
	if err := installPayload(*source, installDir, manifest, manifestBytes); err != nil {
		return err
	}
	binary := filepath.Join(installDir, binaryName("agi-hub"))
	if _, err := os.Lstat(filepath.Join(*data, "config.json")); os.IsNotExist(err) {
		if err := runHub(out, binary, "init", "--data", *data); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	if _, err := app.LoadConfig(*data); err != nil {
		return fmt.Errorf("保留已有配置；请先处理配置错误：%w", err)
	}
	if *endpoint == "" {
		*endpoint = *host
	}
	if (*clientConfig == "") != (*workspace == "") || *clientConfig != "" && *reportBinary != "" {
		return errors.New("client-config 和 workspace 必须一起提供，不能与 report-binary 混用")
	}
	if *clientConfig != "" {
		*reportBinary, err = provisionHelper(installDir, *data, *endpoint, *clientConfig, *workspace)
		if err != nil {
			return err
		}
	}
	if *reportBinary == "" {
		*reportBinary, err = existingReportHelper(*host, *skillsDir)
		if err != nil {
			return err
		}
	}
	installArgs := []string{"install-skill", "--data", *data, "--host", *host, "--endpoint", *endpoint, "--binary", binary}
	if *reportBinary != "" {
		if !filepath.IsAbs(*reportBinary) {
			return errors.New("report-binary 必须为已核实的本机绝对路径")
		}
		info, err := os.Stat(*reportBinary)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return errors.New("report-binary 必须为普通可执行文件")
		}
		installArgs = append(installArgs, "--binary", *reportBinary, "--hub-binary", binary)
	}
	if *skillsDir != "" {
		installArgs = append(installArgs, "--skills-dir", *skillsDir)
	}
	if err := runHub(out, binary, installArgs...); err != nil {
		return err
	}
	if err := runHub(out, binary, "doctor", "--data", *data); err != nil {
		return err
	}
	fmt.Fprintf(out, "\n基础安装完成；尚未接通助手。\n程序：%s\n私有配置：%s\n继续阅读：%s\n", binary, filepath.Join(*data, "config.json"), filepath.Join(installDir, "docs", "INSTALL-FOR-ASSISTANT.md"))
	if *host == "codex" {
		message := fmt.Sprintf("请帮我接入本机 AGI Hub。先读取 %s，运行引导安装器 %s，参数为 --host workbuddy --data %s；路径中的空格须作为完整参数处理。先核实本会话实际技能目录：WorkBuddy 桌面默认目录与独立 CodeBuddy profile 不能混同，非默认时加 --skills-dir 真实技能目录。已有专属回报 helper 时保留它，可显式传 --report-binary 实际程序。保留已有配置、登录态和技能修改，不自行启用未经验证的会话、不修改工具授权。安装后请告诉我本聊天真实会话 ID、当前工作目录、实际 CodeBuddy CLI 入口及配置目录；无法确认的项目保持未知。只做安装核对，不调用其他助手，不发送企业微信消息，不读取聊天历史。", filepath.Join(installDir, "docs", "INSTALL-FOR-ASSISTANT.md"), filepath.Join(installDir, binaryName("agi-hub-setup")), *data)
		fmt.Fprintf(out, "\n可由本人复制到 WorkBuddy 的接入消息（无凭据）：\n%s\n", message)
	}
	return nil
}

func existingReportHelper(host, skillsDir string) (string, error) {
	if skillsDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		skillsDir = filepath.Join(home, "."+host, "skills")
	}
	filename := filepath.Join(skillsDir, "agi-hub-client", "installation.json")
	if _, err := os.Lstat(filename); os.IsNotExist(err) {
		return "", nil
	} else if err != nil {
		return "", err
	}
	body, err := readRegular(filename, 16<<10)
	if err != nil {
		return "", err
	}
	var metadata struct {
		BinaryPath    string `json:"binary_path"`
		HubBinaryPath string `json:"hub_binary_path"`
	}
	if err := json.Unmarshal(body, &metadata); err != nil {
		return "", err
	}
	if metadata.HubBinaryPath != "" {
		return metadata.BinaryPath, nil
	}
	return "", nil
}

func runHub(out io.Writer, binary string, args ...string) error {
	cmd := exec.Command(binary, args...)
	cmd.Stdout, cmd.Stderr = out, out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("Hub 安装步骤未完成：%w", err)
	}
	return nil
}

func binaryName(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

func within(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func noSymlinks(target string) error {
	for current := target; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err == nil && info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("拒绝符号链接路径：%s", current)
		}
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		if filepath.Dir(current) == current {
			return nil
		}
	}
}

func readAndVerify(source string) (packageManifest, []byte, error) {
	var manifest packageManifest
	if err := noSymlinks(source); err != nil {
		return manifest, nil, err
	}
	raw, err := readRegular(filepath.Join(source, "package-manifest.json"), 2<<20)
	if err != nil {
		return manifest, nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return manifest, nil, err
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return manifest, nil, errors.New("安装包 manifest 必须为单个 JSON 对象")
	}
	if manifest.Version != 1 || manifest.Platform != runtime.GOOS+"-"+runtime.GOARCH || len(manifest.Files) == 0 || len(manifest.Files) > 2048 {
		return manifest, nil, errors.New("安装包版本、平台或文件列表不匹配")
	}
	seen := map[string]bool{}
	for _, entry := range manifest.Files {
		if !safePublicPath(entry.Path) || seen[strings.ToLower(entry.Path)] {
			return manifest, nil, fmt.Errorf("非法或重复的安装包文件：%s", entry.Path)
		}
		seen[strings.ToLower(entry.Path)] = true
		if entry.Mode != 0644 && entry.Mode != 0755 {
			return manifest, nil, errors.New("安装包文件权限异常")
		}
		if len(entry.SHA256) != 64 {
			return manifest, nil, errors.New("安装包文件校验值异常")
		}
		body, err := readRegular(filepath.Join(source, filepath.FromSlash(entry.Path)), 64<<20)
		if err != nil {
			return manifest, nil, err
		}
		hash := sha256.Sum256(body)
		if hex.EncodeToString(hash[:]) != entry.SHA256 {
			return manifest, nil, fmt.Errorf("安装包文件校验不符：%s", entry.Path)
		}
	}
	for _, required := range []string{binaryName("agi-hub"), binaryName("agi-hub-setup"), binaryName("agi-hub-endpoint"), "START-HERE.md", "docs/INSTALL-FOR-ASSISTANT.md"} {
		if !seen[strings.ToLower(required)] {
			return manifest, nil, fmt.Errorf("安装包缺少 %s", required)
		}
	}
	return manifest, raw, nil
}

func provisionHelper(installDir, data, endpoint, clientConfig, workspace string) (string, error) {
	if !filepath.IsAbs(clientConfig) || !filepath.IsAbs(workspace) {
		return "", errors.New("client-config 与 workspace 必须为已核实的本机绝对路径")
	}
	if err := noSymlinks(workspace); err != nil {
		return "", err
	}
	info, err := os.Stat(workspace)
	if err != nil || !info.IsDir() {
		return "", errors.New("workspace 必须是已存在的本机目录")
	}
	raw, err := readRegular(clientConfig, 1<<20)
	if err != nil {
		return "", err
	}
	var client struct {
		BaseURL       string          `json:"base_url"`
		Endpoint      string          `json:"endpoint"`
		EndpointToken string          `json:"endpoint_token"`
		Native        json.RawMessage `json:"native"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&client); err != nil {
		return "", errors.New("Client 配置格式错误")
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return "", errors.New("Client 配置必须是单个对象")
	}
	if client.Endpoint != endpoint || len(client.EndpointToken) < 32 {
		return "", errors.New("Client 配置必须属于所选 endpoint 且有其专属凭据")
	}
	hub, err := app.LoadConfig(data)
	if err != nil {
		return "", err
	}
	configured := false
	for _, binding := range hub.Bindings {
		configured = configured || binding.Endpoint.ID == endpoint
	}
	if !configured || client.BaseURL != "http://"+hub.Listen || client.EndpointToken != app.EndpointToken(hub.AdminToken, endpoint) {
		return "", errors.New("Client 配置不属于当前 Hub 的所选 endpoint，保留原配置")
	}
	value := struct {
		Endpoint      string `json:"endpoint"`
		BaseURL       string `json:"base_url"`
		EndpointToken string `json:"endpoint_token"`
		WorkspaceRoot string `json:"workspace_root"`
		DataDir       string `json:"data_dir"`
	}{endpoint, client.BaseURL, client.EndpointToken, filepath.Clean(workspace), data}
	body, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return "", err
	}
	body = append(body, '\n')
	digest := sha256.Sum256(body)
	folder := filepath.Join(data, "endpoint-helpers", hex.EncodeToString(digest[:12]))
	if err := noSymlinks(folder); err != nil {
		return "", err
	}
	source := filepath.Join(installDir, binaryName("agi-hub-endpoint"))
	binary, err := readRegular(source, 64<<20)
	if err != nil {
		return "", err
	}
	// Both binding and executable are immutable. A changed runtime gets its own
	// folder, and custom helpers remain untouched.
	codeHash := sha256.Sum256(binary)
	folder += "-" + hex.EncodeToString(codeHash[:6])
	target := filepath.Join(folder, binaryName("agi-hub-endpoint"))
	if _, err := os.Lstat(folder); err == nil {
		existing, err := readRegular(filepath.Join(folder, "binding.json"), 1<<20)
		if err != nil || !bytes.Equal(existing, body) {
			return "", errors.New("已有专属 helper 配置不符，保留原文件")
		}
		current, err := readRegular(target, 64<<20)
		if err != nil || !bytes.Equal(current, binary) {
			return "", errors.New("已有专属 helper 程序不符，保留原文件")
		}
		return target, nil
	} else if !os.IsNotExist(err) {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(folder), 0700); err != nil {
		return "", err
	}
	stage, err := os.MkdirTemp(filepath.Dir(folder), ".helper-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(stage)
	if err := os.WriteFile(filepath.Join(stage, "binding.json"), body, 0600); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(stage, binaryName("agi-hub-endpoint")), binary, 0755); err != nil {
		return "", err
	}
	if err := os.Rename(stage, folder); err != nil {
		return "", err
	}
	return target, nil
}

func safePublicPath(value string) bool {
	if value == "" || path.IsAbs(value) || path.Clean(value) != value || strings.ContainsAny(value, "\\:\x00") || value == ".." || strings.HasPrefix(value, "../") {
		return false
	}
	for _, part := range strings.Split(value, "/") {
		lower := strings.ToLower(part)
		if part == "." || part == ".." || strings.TrimSpace(part) != part || strings.HasSuffix(part, ".") || lower == "private" || lower == "client.json" || lower == "config.json" || strings.HasPrefix(lower, ".env") {
			return false
		}
		// Windows device filenames are not ordinary files even with extensions.
		base := strings.SplitN(lower, ".", 2)[0]
		if base == "con" || base == "prn" || base == "aux" || base == "nul" || (len(base) == 4 && (strings.HasPrefix(base, "com") || strings.HasPrefix(base, "lpt")) && base[3] >= '1' && base[3] <= '9') {
			return false
		}
	}
	return true
}

func readRegular(filename string, limit int64) ([]byte, error) {
	if err := noSymlinks(filename); err != nil {
		return nil, err
	}
	info, err := os.Lstat(filename)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > limit {
		return nil, fmt.Errorf("拒绝非普通或过大的文件：%s", filename)
	}
	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	body, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, errors.New("安装包文件超过大小限制")
	}
	return body, nil
}

func installPayload(source, target string, manifest packageManifest, raw []byte) error {
	if err := noSymlinks(target); err != nil {
		return err
	}
	if info, err := os.Lstat(target); err == nil {
		if !info.IsDir() {
			return errors.New("安装目标已经存在且不是目录")
		}
		_, existing, err := readAndVerify(target)
		if err != nil {
			return fmt.Errorf("已有安装损坏，保留原目录：%w", err)
		}
		if !bytes.Equal(raw, existing) {
			return errors.New("已有安装版本不符，保留原目录")
		}
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
		return err
	}
	stage, err := os.MkdirTemp(filepath.Dir(target), ".portable-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	for _, entry := range manifest.Files {
		body, err := readRegular(filepath.Join(source, filepath.FromSlash(entry.Path)), 64<<20)
		if err != nil {
			return err
		}
		hash := sha256.Sum256(body)
		if hex.EncodeToString(hash[:]) != entry.SHA256 {
			return fmt.Errorf("安装过程中源文件发生变化：%s", entry.Path)
		}
		targetFile := filepath.Join(stage, filepath.FromSlash(entry.Path))
		if err := os.MkdirAll(filepath.Dir(targetFile), 0700); err != nil {
			return err
		}
		if err := os.WriteFile(targetFile, body, os.FileMode(entry.Mode)); err != nil {
			return err
		}
	}
	if err := os.WriteFile(filepath.Join(stage, "package-manifest.json"), raw, 0644); err != nil {
		return err
	}
	return os.Rename(stage, target)
}
