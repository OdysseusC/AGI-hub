// Build portable packages using only the Go toolchain and standard library.
package main

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
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
	if err := build(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

type buildTarget struct {
	GOOS     string
	PublicOS string
	Arch     string
}

func releaseTargets() []buildTarget {
	return []buildTarget{
		{GOOS: "darwin", PublicOS: "macos", Arch: "amd64"},
		{GOOS: "darwin", PublicOS: "macos", Arch: "arm64"},
		{GOOS: "windows", PublicOS: "windows", Arch: "amd64"},
		{GOOS: "windows", PublicOS: "windows", Arch: "arm64"},
	}
}

func build() error {
	return buildPackages(func(target buildTarget, binary, source string) error {
		cmd := exec.Command("go", "build", "-trimpath", "-buildvcs=false", "-ldflags=-s -w", "-o", binary, source)
		cmd.Env = buildEnvironment(target.GOOS, target.Arch)
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		return cmd.Run()
	})
}

func buildPackages(compile func(buildTarget, string, string) error) error {
	if err := os.MkdirAll("dist", 0755); err != nil {
		return err
	}
	// Remove only outputs of the former release targets. Never discover release
	// inputs through a glob, and never leave an old checksum list on build failure.
	for _, old := range []string{"agi-hub-darwin-amd64.zip", "agi-hub-darwin-arm64.zip", "agi-hub-linux-amd64.zip", "agi-hub-linux-arm64.zip", "SHA256SUMS"} {
		if err := os.Remove(filepath.Join("dist", old)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	var checksums strings.Builder
	for _, target := range releaseTargets() {
		label := target.PublicOS + "-" + target.Arch
		dir := filepath.Join("dist", label)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
		suffix := ""
		if target.GOOS == "windows" {
			suffix = ".exe"
		}
		programs := []struct{ name, source string }{
			{"agi-hub", "./cmd/agi-hub"},
			{"agi-hub-setup", "./tools/bootstrap"},
			{"agi-hub-endpoint", "./tools/endpoint-client"},
		}
		for _, program := range programs {
			if err := compile(target, filepath.Join(dir, program.name+suffix), program.source); err != nil {
				return fmt.Errorf("build %s %s: %w", label, program.name, err)
			}
		}
		archive := filepath.Join("dist", "agi-hub-"+label+".zip")
		name, setupName, helperName := "agi-hub"+suffix, "agi-hub-setup"+suffix, "agi-hub-endpoint"+suffix
		// Public naming stays friendly; bootstrap still compares native GOOS.
		if err := packageBinary(archive, filepath.Join(dir, name), name, filepath.Join(dir, setupName), setupName, filepath.Join(dir, helperName), helperName, target.GOOS+"-"+target.Arch); err != nil {
			return err
		}
		f, err := os.Open(archive)
		if err != nil {
			return err
		}
		hash := sha256.New()
		_, err = io.Copy(hash, f)
		closeErr := f.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		fmt.Fprintf(&checksums, "%x  %s\n", hash.Sum(nil), filepath.Base(archive))
		fmt.Println("Built", label)
	}
	return os.WriteFile("dist/SHA256SUMS", []byte(checksums.String()), 0644)
}

func buildEnvironment(osname, arch string) []string {
	env := []string{}
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if key != "GOOS" && key != "GOARCH" && key != "CGO_ENABLED" {
			env = append(env, entry)
		}
	}
	return append(env, "GOOS="+osname, "GOARCH="+arch, "CGO_ENABLED=0")
}

func packageBinary(path, binary, name, setup, setupName, helper, helperName, platform string) (err error) {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	z := zip.NewWriter(f)
	manifest := packageManifest{Version: 1, Platform: platform, Files: []packageFile{}}
	add := func(source, target string) error {
		linkInfo, e := os.Lstat(source)
		if e != nil {
			return e
		}
		if !linkInfo.Mode().IsRegular() {
			return fmt.Errorf("unexpected package source type: %s", source)
		}
		in, e := os.Open(source)
		if e != nil {
			return e
		}
		defer in.Close()
		info, e := in.Stat()
		if e != nil {
			return e
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("unexpected package source type: %s", source)
		}
		header, e := zip.FileInfoHeader(info)
		if e != nil {
			return e
		}
		header.Name = target
		header.Method = zip.Deflate
		mode := uint32(0644)
		if target == name || target == setupName || target == helperName || target == "install.command" {
			header.SetMode(0755)
			mode = 0755
		} else {
			header.SetMode(0644)
		}
		out, e := z.CreateHeader(header)
		if e != nil {
			return e
		}
		hash := sha256.New()
		_, e = io.Copy(io.MultiWriter(out, hash), in)
		if e == nil {
			manifest.Files = append(manifest.Files, packageFile{Path: target, SHA256: fmt.Sprintf("%x", hash.Sum(nil)), Mode: mode})
		}
		return e
	}
	files := map[string]string{binary: name, setup: setupName, helper: helperName, "START-HERE.md": "START-HERE.md", "INSTALL.md": "INSTALL.md", "README.md": "README.md", "LICENSE": "LICENSE", "config.example.json": "config.example.json", "tools/smoke_client.py": "tools/smoke_client.py"}
	for _, smoke := range []string{"tools/smoke_skill_sync.py", "tools/smoke_mailbox.py", "tools/MAILBOX-SMOKE.md"} {
		files[smoke] = smoke
	}
	for _, document := range []string{"CONTRIBUTING.md", "CHANGELOG.md"} {
		if _, err := os.Lstat(document); err == nil {
			files[document] = document
		} else if !os.IsNotExist(err) {
			z.Close()
			return err
		}
	}
	if strings.HasPrefix(platform, "windows-") {
		files["tools/package/install.ps1"] = "install.ps1"
	} else if strings.HasPrefix(platform, "darwin-") {
		files["tools/package/install.command"] = "install.command"
	} else {
		z.Close()
		return fmt.Errorf("unsupported release platform: %s", platform)
	}
	keys := make([]string, 0, len(files))
	for source := range files {
		keys = append(keys, source)
	}
	sort.Strings(keys)
	for _, source := range keys {
		if err = add(source, files[source]); err != nil {
			z.Close()
			return err
		}
	}
	err = filepath.WalkDir("docs", func(source string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.IsDir() {
			if d.Name() == "private" || strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("unexpected document type: %s", source)
		}
		if filepath.Ext(source) != ".md" {
			return nil
		}
		return add(source, filepath.ToSlash(source))
	})
	if err != nil {
		z.Close()
		return err
	}
	err = filepath.WalkDir("clients", func(source string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.IsDir() {
			if d.Name() == "__pycache__" || d.Name() == "private" || d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		// Explicit public-file allowlist: never package client.json credentials,
		// local caches, logs or arbitrary files alongside an adapter.
		public := filepath.Ext(source) == ".py" || filepath.Ext(source) == ".mjs" || filepath.Ext(source) == ".md" || d.Name() == "manifest.json" || d.Name() == "client.example.json"
		relative := filepath.ToSlash(source)
		if strings.HasPrefix(relative, "clients/claude-code/") {
			public = public || filepath.Ext(source) == ".js" || relative == "clients/claude-code/.claude-plugin/marketplace.json" || relative == "clients/claude-code/agi-hub-endpoint/.claude-plugin/plugin.json" || relative == "clients/claude-code/agi-hub-endpoint/hooks/hooks.json"
		}
		if !public {
			return nil
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("unexpected client source type: %s", source)
		}
		return add(source, filepath.ToSlash(source))
	})
	if err != nil {
		z.Close()
		return err
	}
	sort.Slice(manifest.Files, func(i, j int) bool { return manifest.Files[i].Path < manifest.Files[j].Path })
	body, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		z.Close()
		return err
	}
	writer, err := z.Create("package-manifest.json")
	if err != nil {
		z.Close()
		return err
	}
	if _, err = writer.Write(append(body, '\n')); err != nil {
		z.Close()
		return err
	}
	return z.Close()
}
