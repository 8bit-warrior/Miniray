//go:build linux

package xray

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const coreUnit = "miniray-core.service"

func independentCoreEnabled() bool { return os.Getenv("MINIRAY_SEPARATE_CORE") == "1" }
func corePID() (int, error) {
	out, err := exec.CommandContext(context.Background(), "systemctl", "show", "--property=MainPID", "--value", coreUnit).Output()
	if err != nil {
		return 0, fmt.Errorf("query core service: %w", err)
	}
	return strconv.Atoi(strings.TrimSpace(string(out)))
}

func verifyCorePID(pid int) error {
	if pid <= 0 {
		return errors.New("core is not running")
	}
	exe, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid))
	if err != nil {
		return err
	}
	expected, err := filepath.EvalSymlinks(GetBinaryPath())
	if err != nil {
		return err
	}
	actual, err := filepath.EvalSymlinks(exe)
	if err != nil {
		return err
	}
	if actual != expected {
		return fmt.Errorf("refusing to manage unrelated core PID %d", pid)
	}
	return nil
}

func (p *process) adoptCore(pid int) error {
	if err := verifyCorePID(pid); err != nil {
		return err
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	done := make(chan struct{})
	p.mu.Lock()
	p.cmd = &exec.Cmd{Process: proc}
	p.done = done
	p.managedCore = true
	p.startTime = time.Now()
	p.mu.Unlock()
	p.refreshVersion()
	p.refreshAPIPort()
	go func() {
		timer := time.NewTicker(time.Second)
		defer timer.Stop()
		defer close(done)
		for range timer.C {
			live, err := corePID()
			if err != nil || live != pid || syscall.Kill(pid, 0) != nil {
				return
			}
		}
	}()
	return nil
}

func (p *process) startIndependentCore(configPath string, oldData, newData []byte) error {
	pid, err := corePID()
	if err != nil {
		return err
	}
	if pid > 0 {
		if err := verifyCorePID(pid); err != nil {
			return err
		}
		if bytes.Equal(oldData, newData) {
			return p.adoptCore(pid)
		}
	}
	if err := writeFileAtomic(configPath, newData, 0o600); err != nil {
		return err
	}
	action := "start"
	if pid > 0 {
		action = "restart"
	}
	if out, err := exec.CommandContext(context.Background(), "systemctl", action, coreUnit).CombinedOutput(); err != nil {
		return fmt.Errorf("systemctl %s: %w: %s", action, err, out)
	}
	var last error
	for i := 0; i < 30; i++ {
		time.Sleep(200 * time.Millisecond)
		live, err := corePID()
		if err == nil && live > 0 {
			if err = p.adoptCore(live); err == nil {
				return nil
			}
		}
		last = err
	}
	return fmt.Errorf("core did not start or pass identity verification: %w", last)
}

func stopIndependentCore() error {
	if out, err := exec.CommandContext(context.Background(), "systemctl", "stop", coreUnit).CombinedOutput(); err != nil {
		return fmt.Errorf("stop core: %w: %s", err, out)
	}
	return nil
}
