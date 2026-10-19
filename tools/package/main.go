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

func build() error {
	if err := os.MkdirAll("dist", 0755); err != nil {
		return err
	}
	var checksums strings.Builder
	for _, osname := range []string{"darwin", "windows", "linux"} {
		for _, arch := range []string{"amd64", "arm64"} {
			label := osname + "-" + arch
			dir := filepath.Join("dist", label)
			if err := os.MkdirAll(dir, 0755); err != nil {
				return err
			}
			name := "agi-hub"
			if osname == "windows" {
				name += ".exe"
			}
			binary := filepath.Join(dir, name)
			cmd := exec.Command("go", "build", "-trimpath", "-buildvcs=false", "-ldflags=-s -w", "-o", binary, "./cmd/agi-hub")
			cmd.Env = buildEnvironment(osname, arch)
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			if err := cmd.Run(); err != nil {
				return fmt.Errorf("build %s: %w", label, err)
			}
			setupName := "agi-hub-setup"
			if osname == "windows" {
				setupName += ".exe"
			}
			setup := filepath.Join(dir, setupName)
			setupCmd := exec.Command("go", "build", "-trimpath", "-buildvcs=false", "-ldflags=-s -w", "-o", setup, "./tools/bootstrap")
			setupCmd.Env = buildEnvironment(osname, arch)
			setupCmd.Stdout, setupCmd.Stderr = os.Stdout, os.Stderr
			if err := setupCmd.Run(); err != nil {
				return fmt.Errorf("build setup %s: %w", label, err)
			}
			helperName := "agi-hub-endpoint"
			if osname == "windows" {
				helperName += ".exe"
			}
			helper := filepath.Join(dir, helperName)
			helperCmd := exec.Command("go", "build", "-trimpath", "-buildvcs=false", "-ldflags=-s -w", "-o", helper, "./tools/endpoint-client")
			helperCmd.Env = buildEnvironment(osname, arch)
			helperCmd.Stdout, helperCmd.Stderr = os.Stdout, os.Stderr
			if err := helperCmd.Run(); err != nil {
				return fmt.Errorf("build endpoint helper %s: %w", label, err)
			}
			archive := filepath.Join("dist", "agi-hub-"+label+".zip")
			if err := packageBinary(archive, binary, name, setup, setupName, helper, helperName, label); err != nil {
				return err
			}
			f, err := os.Open(archive)
			if err != nil {
				return err
			}
			hash := sha256.New()
			_, err = io.Copy(hash, f)
			f.Close()
			if err != nil {
				return err
			}
			fmt.Fprintf(&checksums, "%x  %s\n", hash.Sum(nil), filepath.Base(archive))
			fmt.Println("Built", label)
		}
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
		if target == name || target == setupName || target == helperName || target == "install.command" || target == "install.sh" {
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
		files["tools/package/install.sh"] = "install.sh"
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
