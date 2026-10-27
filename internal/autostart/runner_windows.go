package autostart

import (
	"io"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

var startupKernel32 = syscall.NewLazyDLL("kernel32.dll")
var startupCreateJob = startupKernel32.NewProc("CreateJobObjectW")
var startupSetJob = startupKernel32.NewProc("SetInformationJobObject")
var startupAssignJob = startupKernel32.NewProc("AssignProcessToJobObject")
var startupConsoleEvent = startupKernel32.NewProc("GenerateConsoleCtrlEvent")
var startupThreadSnapshot = startupKernel32.NewProc("CreateToolhelp32Snapshot")
var startupThreadFirst = startupKernel32.NewProc("Thread32First")
var startupThreadNext = startupKernel32.NewProc("Thread32Next")
var startupOpenThread = startupKernel32.NewProc("OpenThread")
var startupResumeThread = startupKernel32.NewProc("ResumeThread")

type startupBasicJobLimits struct {
	ProcessTimeLimit int64
	JobTimeLimit     int64
	Flags            uint32
	MinWorkingSet    uintptr
	MaxWorkingSet    uintptr
	ActiveProcesses  uint32
	Affinity         uintptr
	PriorityClass    uint32
	SchedulingClass  uint32
}

type startupJobLimits struct {
	Basic          startupBasicJobLimits
	IOCounters     [6]uint64
	ProcessMemory  uintptr
	JobMemory      uintptr
	PeakProcessMem uintptr
	PeakJobMem     uintptr
}

type processGuard struct {
	job   syscall.Handle
	once  *sync.Once
	group uint32
}

func configureOwnedCommand(cmd *exec.Cmd) {
	// A separate console group allows a best-effort graceful CTRL_BREAK. The
	// initial thread remains suspended until its private Job Object owns it,
	// so even a fast child cannot spawn outside our process-tree ownership.
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x00000200 | 0x00000004}
}

func guardOwnedProcess(cmd *exec.Cmd) (processGuard, error) {
	job, _, callErr := startupCreateJob.Call(0, 0)
	if job == 0 {
		return processGuard{}, startupWindowsError(callErr)
	}
	guard := processGuard{job: syscall.Handle(job), once: &sync.Once{}, group: uint32(cmd.Process.Pid)}
	limits := startupJobLimits{}
	limits.Basic.Flags = 0x00002000 // JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	result, _, callErr := startupSetJob.Call(job, 9, uintptr(unsafe.Pointer(&limits)), unsafe.Sizeof(limits))
	runtime.KeepAlive(&limits)
	if result == 0 {
		syscall.CloseHandle(guard.job)
		return processGuard{}, startupWindowsError(callErr)
	}
	process, err := syscall.OpenProcess(0x0100|0x0001, false, guard.group) // SET_QUOTA | TERMINATE
	if err != nil {
		syscall.CloseHandle(guard.job)
		return processGuard{}, err
	}
	defer syscall.CloseHandle(process)
	result, _, callErr = startupAssignJob.Call(job, uintptr(process))
	if result == 0 {
		syscall.CloseHandle(guard.job)
		return processGuard{}, startupWindowsError(callErr)
	}
	if err := resumeStartupProcess(guard.group); err != nil {
		syscall.CloseHandle(guard.job)
		return processGuard{}, err
	}
	return guard, nil
}

type startupThreadEntry struct {
	Size           uint32
	Usage          uint32
	ThreadID       uint32
	OwnerProcessID uint32
	BasePriority   int32
	DeltaPriority  int32
	Flags          uint32
}

func resumeStartupProcess(pid uint32) error {
	snapshot, _, callErr := startupThreadSnapshot.Call(0x00000004, 0) // TH32CS_SNAPTHREAD
	if snapshot == ^uintptr(0) {
		return startupWindowsError(callErr)
	}
	defer syscall.CloseHandle(syscall.Handle(snapshot))
	entry := startupThreadEntry{}
	entry.Size = uint32(unsafe.Sizeof(entry))
	result, _, callErr := startupThreadFirst.Call(snapshot, uintptr(unsafe.Pointer(&entry)))
	for result != 0 {
		if entry.OwnerProcessID == pid {
			thread, _, err := startupOpenThread.Call(0x0002, 0, uintptr(entry.ThreadID)) // THREAD_SUSPEND_RESUME
			if thread == 0 {
				return startupWindowsError(err)
			}
			defer syscall.CloseHandle(syscall.Handle(thread))
			previous, _, err := startupResumeThread.Call(thread)
			if previous == uintptr(^uint32(0)) {
				return startupWindowsError(err)
			}
			return nil
		}
		entry.Size = uint32(unsafe.Sizeof(entry))
		result, _, callErr = startupThreadNext.Call(snapshot, uintptr(unsafe.Pointer(&entry)))
	}
	runtime.KeepAlive(&entry)
	return startupWindowsError(callErr)
}

func startupWindowsError(err error) error {
	if err == syscall.Errno(0) {
		return syscall.EINVAL
	}
	return err
}

func (g processGuard) stop(process *os.Process, done <-chan struct{}, grace time.Duration) {
	g.once.Do(func() {
		_, _, _ = startupConsoleEvent.Call(1, uintptr(g.group)) // CTRL_BREAK_EVENT
		timer := time.NewTimer(grace)
		defer timer.Stop()
		select {
		case <-done:
		case <-timer.C:
		}
		// Closing the private kill-on-close job also stops descendants when
		// the group leader has already exited or graceful signalling failed.
		_ = syscall.CloseHandle(g.job)
		<-done
	})
}

func openRuntimeLog(path string) (*os.File, error) {
	file, err := openRuntimeLockFile(path)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err == nil {
		var current os.FileInfo
		current, err = os.Lstat(path)
		if err == nil && (!info.Mode().IsRegular() || !current.Mode().IsRegular() || !os.SameFile(info, current)) {
			err = syscall.EINVAL
		}
	}
	if err == nil {
		err = file.Chmod(0600)
	}
	if err == nil {
		_, err = file.Seek(0, io.SeekEnd)
	}
	if err != nil {
		file.Close()
		return nil, err
	}
	return file, nil
}
