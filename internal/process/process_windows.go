//go:build windows

package process

import (
	"errors"
	"fmt"
	"os/exec"
	"syscall"
	"time"
	"unsafe"
)

var kernel32 = syscall.NewLazyDLL("kernel32.dll")
var (
	createJob      = kernel32.NewProc("CreateJobObjectW")
	setJobInfo     = kernel32.NewProc("SetInformationJobObject")
	queryJobInfo   = kernel32.NewProc("QueryInformationJobObject")
	assignJob      = kernel32.NewProc("AssignProcessToJobObject")
	terminateJob   = kernel32.NewProc("TerminateJobObject")
	createSnapshot = kernel32.NewProc("CreateToolhelp32Snapshot")
	threadFirst    = kernel32.NewProc("Thread32First")
	threadNext     = kernel32.NewProc("Thread32Next")
	openThread     = kernel32.NewProc("OpenThread")
	resumeThread   = kernel32.NewProc("ResumeThread")
)

type basicLimits struct {
	ProcessTime, JobTime           int64
	Flags                          uint32
	MinWorkingSet, MaxWorkingSet   uintptr
	ActiveProcessLimit             uint32
	Affinity                       uintptr
	PriorityClass, SchedulingClass uint32
}
type extendedLimits struct {
	Basic                                                      basicLimits
	IOCounters                                                 [6]uint64
	ProcessMemory, JobMemory, PeakProcessMemory, PeakJobMemory uintptr
}
type accounting struct {
	UserTime, KernelTime, PeriodUserTime, PeriodKernelTime           int64
	PageFaults, TotalProcesses, ActiveProcesses, TerminatedProcesses uint32
}
type threadEntry struct {
	Size, Usage, ID, OwnerPID   uint32
	BasePriority, DeltaPriority int32
	Flags                       uint32
}
type platformGuard struct{ job syscall.Handle }

func winError(op string, err error) error {
	if err == syscall.Errno(0) {
		err = syscall.EINVAL
	}
	return fmt.Errorf("%s: %w", op, err)
}
func prepare(cmd *exec.Cmd) (*platformGuard, error) {
	// A nil security descriptor creates a non-inheritable handle. Without
	// suspended startup a child could spawn descendants before Job assignment.
	h, _, err := createJob.Call(0, 0)
	if h == 0 {
		return nil, winError("CreateJobObject", err)
	}
	g := &platformGuard{job: syscall.Handle(h)}
	limits := extendedLimits{}
	limits.Basic.Flags = 0x2000 // JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE; no breakaway.
	ok, _, err := setJobInfo.Call(h, 9, uintptr(unsafe.Pointer(&limits)), unsafe.Sizeof(limits))
	if ok == 0 {
		g.close()
		return nil, winError("SetInformationJobObject", err)
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= 0x4 // CREATE_SUSPENDED; retain controlled CmdLine.
	return g, nil
}
func (g *platformGuard) started(pid int) error {
	h, err := syscall.OpenProcess(0x0100|0x0001, false, uint32(pid)) // SET_QUOTA | TERMINATE.
	if err != nil {
		return err
	}
	defer syscall.CloseHandle(h)
	ok, _, callErr := assignJob.Call(uintptr(g.job), uintptr(h))
	if ok == 0 {
		return winError("AssignProcessToJobObject (no fallback)", callErr)
	}
	snapshot, _, callErr := createSnapshot.Call(0x4, 0) // TH32CS_SNAPTHREAD.
	if snapshot == ^uintptr(0) {
		return winError("CreateToolhelp32Snapshot", callErr)
	}
	defer syscall.CloseHandle(syscall.Handle(snapshot))
	entry := threadEntry{Size: uint32(unsafe.Sizeof(threadEntry{}))}
	ok, _, callErr = threadFirst.Call(snapshot, uintptr(unsafe.Pointer(&entry)))
	for ok != 0 {
		if entry.OwnerPID == uint32(pid) {
			thread, _, err := openThread.Call(0x2, 0, uintptr(entry.ID)) // SUSPEND_RESUME.
			if thread == 0 {
				return winError("OpenThread", err)
			}
			previous, _, err := resumeThread.Call(thread)
			syscall.CloseHandle(syscall.Handle(thread))
			if previous == 0xffffffff {
				return winError("ResumeThread", err)
			}
			return nil
		}
		entry.Size = uint32(unsafe.Sizeof(entry))
		ok, _, callErr = threadNext.Call(snapshot, uintptr(unsafe.Pointer(&entry)))
	}
	return errors.New("suspended process primary thread not found")
}

// Console signals have tool-specific requirements. The generic Windows
// fallback is Job termination; protocol-level cancellation belongs to agent.
func (g *platformGuard) terminate() error { return g.kill() }
func (g *platformGuard) kill() error {
	ok, _, err := terminateJob.Call(uintptr(g.job), 1)
	if ok == 0 {
		return winError("TerminateJobObject", err)
	}
	return nil
}
func (g *platformGuard) active() (uint32, error) {
	var a accounting
	ok, _, err := queryJobInfo.Call(uintptr(g.job), 1, uintptr(unsafe.Pointer(&a)), unsafe.Sizeof(a), 0)
	if ok == 0 {
		return 0, winError("QueryInformationJobObject", err)
	}
	return a.ActiveProcesses, nil
}
func (g *platformGuard) cleanup(grace time.Duration) (bool, error) {
	active, err := g.active()
	if err != nil {
		return false, err
	}
	if active == 0 {
		return true, nil
	}
	if err = g.kill(); err != nil {
		return false, err
	}
	deadline := time.Now().Add(grace + time.Second)
	for time.Now().Before(deadline) {
		active, err = g.active()
		if err != nil {
			return false, err
		}
		if active == 0 {
			return true, nil
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false, nil
}
func (g *platformGuard) close() {
	if g.job != 0 {
		syscall.CloseHandle(g.job)
		g.job = 0
	}
}

// TreeQuiescent is for a recorded command whose sole, non-inheritable Job
// handle belonged to this Runner. On owner exit that Job kills descendants.
func TreeQuiescent(pid int) (bool, error) {
	if pid <= 0 {
		return false, errors.New("missing recorded process identity")
	}
	h, err := syscall.OpenProcess(0x1000, false, uint32(pid)) // QUERY_LIMITED_INFORMATION.
	if err != nil {
		if errors.Is(err, syscall.Errno(87)) { // ERROR_INVALID_PARAMETER (PID absent).
			return true, nil
		}
		return false, err
	}
	defer syscall.CloseHandle(h)
	var code uint32
	if err := syscall.GetExitCodeProcess(h, &code); err != nil {
		return false, err
	}
	return code != 259, nil // STILL_ACTIVE.
}
