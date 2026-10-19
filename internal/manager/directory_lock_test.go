package manager

import (
	"bufio"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

const expectedDirectoryLockFilename = ".agi-hub.lock"

func TestDirectoryLockDoesNotDependOnHashedTCPPort(t *testing.T) {
	root := t.TempDir()
	seen := map[int]string{}
	var first, second string
	// Deterministically find two different paths which shared the old TCP guard.
	// A real kernel file lock must permit these directories to run independently.
	for i := 0; i < 40001; i++ {
		dir := filepath.Join(root, fmt.Sprintf("directory-%d", i))
		key := dir
		if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
			key = strings.ToLower(key)
		}
		digest := sha256.Sum256([]byte(key))
		port := 20000 + (int(digest[0])<<8|int(digest[1]))%40000
		if previous, ok := seen[port]; ok {
			first, second = previous, dir
			break
		}
		seen[port] = dir
	}
	if first == "" {
		t.Fatal("could not construct legacy port collision")
	}
	for _, dir := range []string{first, second} {
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	lockOne, err := acquireDirectoryLock(first)
	if err != nil {
		t.Fatalf("first directory must not depend on TCP port availability: %v", err)
	}
	defer lockOne.Close()
	lockTwo, err := acquireDirectoryLock(second)
	if err != nil {
		t.Fatalf("independent directories must not collide through a hashed TCP port: %v", err)
	}
	defer lockTwo.Close()
	duplicate, err := acquireDirectoryLock(first)
	if err == nil {
		duplicate.Close()
		t.Fatal("same directory acquired twice")
	}
}

func TestDirectoryLockPersistsInodeAndReleasesOnClose(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, expectedDirectoryLockFilename)
	first, err := acquireDirectoryLock(dir)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		first.Close()
		t.Fatalf("kernel lock must use a persistent file: %v", err)
	}
	if !info.Mode().IsRegular() {
		first.Close()
		t.Fatal("lock path is not a regular file")
	}
	if err = first.Close(); err != nil {
		t.Fatal(err)
	}
	second, err := acquireDirectoryLock(dir)
	if err != nil {
		t.Fatalf("closed owner retained lock: %v", err)
	}
	defer second.Close()
	after, err := os.Lstat(path)
	if err != nil || !os.SameFile(info, after) {
		t.Fatal("release removed or replaced the persistent lock inode")
	}
}

func TestDirectoryLockRejectsNonregularPath(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, expectedDirectoryLockFilename), 0700); err != nil {
		t.Fatal(err)
	}
	lock, err := acquireDirectoryLock(dir)
	if err == nil {
		lock.Close()
		t.Fatal("directory was accepted as a regular lock file")
	}
}

func TestDirectoryLockProcessHelper(t *testing.T) {
	if os.Getenv("AGI_HUB_DIRECTORY_LOCK_HELPER") != "1" {
		return
	}
	lock, err := acquireDirectoryLock(os.Getenv("AGI_HUB_DIRECTORY_LOCK_PATH"))
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	fmt.Fprintln(os.Stdout, "LOCKED")
	// The parent keeps this pipe open and kills this process. No deferred Close
	// runs on that path; reacquisition therefore checks kernel crash cleanup.
	_, _ = bufio.NewReader(os.Stdin).ReadByte()
}

func TestDirectoryLockRejectsOtherProcessAndRecoversAfterCrash(t *testing.T) {
	dir := t.TempDir()
	command := exec.Command(os.Args[0], "-test.run=^TestDirectoryLockProcessHelper$")
	command.Env = append(os.Environ(), "AGI_HUB_DIRECTORY_LOCK_HELPER=1", "AGI_HUB_DIRECTORY_LOCK_PATH="+dir)
	output, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	input, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	if err = command.Start(); err != nil {
		t.Fatal(err)
	}
	waited := false
	defer func() {
		if !waited {
			_ = command.Process.Kill()
			_ = command.Wait()
		}
	}()
	ready := make(chan string, 1)
	go func() { line, _ := bufio.NewReader(output).ReadString('\n'); ready <- strings.TrimSpace(line) }()
	select {
	case line := <-ready:
		if line != "LOCKED" {
			t.Fatalf("child did not acquire lock: %q", line)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("child lock acquisition timed out")
	}
	duplicate, err := acquireDirectoryLock(dir)
	if err == nil {
		duplicate.Close()
		t.Fatal("second process entered an owned directory")
	}
	if err = command.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = command.Wait()
	waited = true
	deadline := time.Now().Add(5 * time.Second)
	for {
		lock, err := acquireDirectoryLock(dir)
		if err == nil {
			defer lock.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("kernel did not release crashed owner's lock: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err = os.Lstat(filepath.Join(dir, expectedDirectoryLockFilename)); err != nil {
		t.Fatal("crash recovery depended on deleting lock file", err)
	}
}
