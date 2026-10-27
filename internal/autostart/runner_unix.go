//go:build darwin || linux

package autostart

import (
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

type processGuard struct {
	group int
	once  *sync.Once
}

func configureOwnedCommand(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func guardOwnedProcess(cmd *exec.Cmd) (processGuard, error) {
	return processGuard{group: cmd.Process.Pid, once: &sync.Once{}}, nil
}

func (g processGuard) stop(process *os.Process, done <-chan struct{}, grace time.Duration) {
	g.once.Do(func() {
		// Only a process group created for this child is signalled. Even after
		// its leader exits, remaining descendants belong to the same group.
		_ = syscall.Kill(-g.group, syscall.SIGTERM)
		deadline := time.NewTimer(grace)
		defer deadline.Stop()
		tick := time.NewTicker(10 * time.Millisecond)
		defer tick.Stop()
		for syscall.Kill(-g.group, 0) == nil {
			select {
			case <-deadline.C:
				_ = syscall.Kill(-g.group, syscall.SIGKILL)
				<-done
				return
			case <-tick.C:
			}
		}
		<-done
	})
}

func openRuntimeLog(path string) (*os.File, error) {
	fd, err := syscall.Open(path, syscall.O_CREAT|syscall.O_WRONLY|syscall.O_APPEND|syscall.O_CLOEXEC|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0600)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	info, err := file.Stat()
	if err == nil && !info.Mode().IsRegular() {
		err = syscall.EINVAL
	}
	if err == nil {
		err = file.Chmod(0600)
	}
	if err != nil {
		file.Close()
		return nil, err
	}
	return file, nil
}
