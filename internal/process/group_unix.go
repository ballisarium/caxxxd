//go:build unix

// Package process contains the Unix process-group plumbing that lets caxxxd
// stop a download together with every helper it spawned, such as ffmpeg.
package process

import (
	"errors"
	"os/exec"
	"syscall"
)

// ConfigureGroup puts the command in its own process group so its children can
// be signalled as a unit.
func ConfigureGroup(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

// TerminateGroup sends SIGTERM to the whole process group of a started command.
func TerminateGroup(cmd *exec.Cmd) error {
	return signalGroup(cmd, syscall.SIGTERM)
}

// KillGroup sends SIGKILL to the whole process group. It is the escalation for
// a child that ignores SIGTERM, so a cancelled download never leaves an ffmpeg
// running in the background.
func KillGroup(cmd *exec.Cmd) error {
	return signalGroup(cmd, syscall.SIGKILL)
}

func signalGroup(cmd *exec.Cmd, signal syscall.Signal) error {
	if cmd == nil || cmd.Process == nil {
		return errors.New("process has not started")
	}
	return syscall.Kill(-cmd.Process.Pid, signal)
}
