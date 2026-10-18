//go:build windows

package xray

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	gprocess "github.com/shirou/gopsutil/v4/process"

	"github.com/8bit-warrior/Miniray/internal/config"
)

func independentCoreEnabled() bool { return true }
func corePIDFile() string          { return filepath.Join(config.GetBinFolderPath(), "miniray-core.pid") }

func corePID() (int, error) {
	data, err := os.ReadFile(corePIDFile())
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(data)))
}

func verifyCorePID(pid int) error {
	if pid <= 0 {
		return fmt.Errorf("invalid proxy PID")
	}
	p, err := gprocess.NewProcess(int32(pid))
	if err != nil {
		return err
	}
	exe, err := p.Exe()
	if err != nil {
		return err
	}
	expected, err := filepath.Abs(GetBinaryPath())
	if err != nil {
		return err
	}
	if !strings.EqualFold(filepath.Clean(expected), filepath.Clean(exe)) {
		return fmt.Errorf("refusing to adopt or kill unrelated PID %d", pid)
	}
	return nil
}

func (p *process) adoptCore(pid int) error {
	if err := verifyCorePID(pid); err != nil {
		return err
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	done := make(chan struct{})
	p.mu.Lock()
	p.cmd = &exec.Cmd{Process: process}
	p.done = done
	p.managedCore = true
	p.startTime = time.Now()
	p.mu.Unlock()
	p.refreshVersion()
	p.refreshAPIPort()
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		defer close(done)
		for range ticker.C {
			live, err := corePID()
			if err != nil || live != pid || verifyCorePID(pid) != nil {
				return
			}
		}
	}()
	return nil
}

func IndependentCoreManuallyStopped() bool         { return false }
func SetIndependentCoreManuallyStopped(bool) error { return nil }
func StartIndependentAgent() error                 { return nil }

func IndependentCoreRunning() bool {
	pid, err := corePID()
	return err == nil && pid > 0 && verifyCorePID(pid) == nil
}

func StopIndependentCore() error {
	return stopIndependentCore()
}

func (p *process) startIndependentCore(path string, oldConfig, newConfig []byte, forceRestart bool) error {
	pid, err := corePID()
	if err != nil {
		return err
	}
	if pid > 0 {
		if err := verifyCorePID(pid); err == nil {
			if !forceRestart && bytes.Equal(oldConfig, newConfig) {
				return p.adoptCore(pid)
			}
			proc, err := os.FindProcess(pid)
			if err != nil {
				return err
			}
			if err = proc.Kill(); err != nil {
				return err
			}
		} else {
			// A stale PID is not a valid running core. Do not terminate unknown PIDs.
			if _, statErr := gprocess.NewProcess(int32(pid)); statErr == nil {
				return fmt.Errorf("untrusted running PID %d", pid)
			}
			_ = os.Remove(corePIDFile())
		}
	}
	if err := writeFileAtomic(path, newConfig, 0o600); err != nil {
		return err
	}
	if err := os.MkdirAll(config.GetLogFolder(), 0o700); err != nil {
		return err
	}
	out, err := os.OpenFile(filepath.Join(config.GetLogFolder(), "core-stdout.log"), os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer out.Close()
	cmd := exec.Command(GetBinaryPath(), "-c", path)
	attachChildLifetime(cmd)
	cmd.Stdout = out
	cmd.Stderr = out
	if err := cmd.Start(); err != nil {
		return err
	}
	pid = cmd.Process.Pid
	if err := os.WriteFile(corePIDFile(), []byte(strconv.Itoa(pid)), 0o600); err != nil {
		return err
	}
	done := make(chan struct{})
	p.mu.Lock()
	p.cmd = cmd
	p.done = done
	p.managedCore = true
	p.startTime = time.Now()
	p.mu.Unlock()
	go func() { _ = cmd.Wait(); close(done) }()
	p.refreshVersion()
	p.refreshAPIPort()
	return nil
}

func stopIndependentCore() error {
	pid, err := corePID()
	if err != nil || pid == 0 {
		return err
	}
	if err := verifyCorePID(pid); err != nil {
		return err
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	if err := proc.Kill(); err != nil {
		return err
	}
	return os.Remove(corePIDFile())
}
