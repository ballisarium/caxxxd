//go:build unix

package process_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/ballisarium/caxxxd/internal/process"
)

func TestConfigureGroupSetsProcessGroup(t *testing.T) {
	cmd := exec.Command("true")
	process.ConfigureGroup(cmd)

	if cmd.SysProcAttr == nil || !cmd.SysProcAttr.Setpgid {
		t.Fatalf("SysProcAttr = %#v, want Setpgid", cmd.SysProcAttr)
	}
}

func TestConfigureGroupKeepsExistingAttributes(t *testing.T) {
	cmd := exec.Command("true")
	cmd.SysProcAttr = &syscall.SysProcAttr{Foreground: false}
	process.ConfigureGroup(cmd)

	if !cmd.SysProcAttr.Setpgid {
		t.Fatal("ConfigureGroup must not discard existing attributes")
	}
}

func TestTerminateGroupRejectsUnstartedCommand(t *testing.T) {
	if err := process.TerminateGroup(exec.Command("true")); err == nil {
		t.Fatal("TerminateGroup on an unstarted command must fail")
	}
	if err := process.TerminateGroup(nil); err == nil {
		t.Fatal("TerminateGroup(nil) must fail")
	}
}

func TestTerminateGroupStopsChildren(t *testing.T) {
	script := filepath.Join(t.TempDir(), "spawner")
	body := "#!/bin/sh\nsleep 30 &\nwait\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	cmd := exec.Command(script)
	process.ConfigureGroup(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}

	if err := process.TerminateGroup(cmd); err != nil {
		t.Fatalf("TerminateGroup: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("process group survived SIGTERM")
	}
}
