package app

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/creack/pty"
	"golang.org/x/term"

	"github.com/ballisarium/caxxxd/internal/ui"
)

func TestMenuInterruptHelper(t *testing.T) {
	if os.Getenv("CAXXXD_TEST_MENU") != "1" {
		return
	}
	console := ui.NewConsole(os.Stdout)
	defer console.Restore()
	_, err := NewTerminalPrompter(console).Choose("Pick a source", []Choice{
		{Label: "Link"}, {Label: "Browser"},
	}, 0)
	if !errors.Is(err, ErrInterrupted) {
		t.Fatalf("Choose returned %v, want ErrInterrupted", err)
	}
}

func TestMenuBatchedCtrlCReturnsToTerminal(t *testing.T) {
	master, slave, err := pty.Open()
	if err != nil {
		t.Skipf("no pty available: %v", err)
	}
	defer master.Close()
	defer slave.Close()
	if err := pty.Setsize(master, &pty.Winsize{Rows: 24, Cols: 100}); err != nil {
		t.Fatal(err)
	}
	before, err := term.GetState(int(master.Fd()))
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(os.Args[0], "-test.run=^TestMenuInterruptHelper$", "-test.count=1")
	command.Env = append(os.Environ(), "CAXXXD_TEST_MENU=1")
	command.Stdin, command.Stdout, command.Stderr = slave, slave, slave
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	t.Cleanup(func() { _ = command.Process.Kill() })

	var mutex sync.Mutex
	var output strings.Builder
	go func() {
		buffer := make([]byte, 4096)
		for {
			n, err := master.Read(buffer)
			mutex.Lock()
			output.Write(buffer[:n])
			mutex.Unlock()
			if err != nil {
				return
			}
		}
	}()
	ready := false
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		mutex.Lock()
		shown := strings.Contains(output.String(), "Pick a source")
		mutex.Unlock()
		state, err := term.GetState(int(master.Fd()))
		if err == nil && shown && !reflect.DeepEqual(state, before) {
			ready = true
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !ready {
		t.Fatal("menu never accepted raw terminal input")
	}
	if _, err := io.WriteString(master, "\x03\x03"); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("menu process: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("batched Ctrl+C did not exit the menu")
	}
	after, err := term.GetState(int(master.Fd()))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatal("menu did not restore terminal settings")
	}
}
