// Build portable packages using only the Go toolchain and standard library.
package main

import (
	"archive/zip"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

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
			archive := filepath.Join("dist", "agi-hub-"+label+".zip")
			if err := packageBinary(archive, binary, name); err != nil {
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

func packageBinary(path, binary, name string) (err error) {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	z := zip.NewWriter(f)
	add := func(source, target string) error {
		in, e := os.Open(source)
		if e != nil {
			return e
		}
		defer in.Close()
		info, e := in.Stat()
		if e != nil {
			return e
		}
		header, e := zip.FileInfoHeader(info)
		if e != nil {
			return e
		}
		header.Name = target
		header.Method = zip.Deflate
		if target == name {
			header.SetMode(0755)
		}
		out, e := z.CreateHeader(header)
		if e != nil {
			return e
		}
		_, e = io.Copy(out, in)
		return e
	}
	for source, target := range map[string]string{binary: name, "README.md": "README.md", "LICENSE": "LICENSE", "config.example.json": "config.example.json"} {
		if err = add(source, target); err != nil {
			z.Close()
			return err
		}
	}
	err = filepath.WalkDir("docs", func(source string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("unexpected document type: %s", source)
		}
		return add(source, filepath.ToSlash(source))
	})
	if err != nil {
		z.Close()
		return err
	}
	return z.Close()
}
