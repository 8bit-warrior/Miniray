//go:build linux

package mtproto

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// killStrayMtgProcesses stops processes running an mtg configuration owned by
// this installation. A matching executable name alone does not prove ownership.
func killStrayMtgProcesses(binaryPath string) int {
	expected, err := filepath.Abs(binaryPath)
	if err != nil {
		return 0
	}
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0
	}
	killed := 0
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid == os.Getpid() {
			continue
		}
		exe, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid))
		if err != nil || strings.TrimSuffix(exe, " (deleted)") != expected {
			continue
		}
		cmdline, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
		if err != nil || !isOwnedMtgCommand(cmdline, binaryPath) {
			continue
		}
		if err := syscall.Kill(pid, syscall.SIGKILL); err == nil {
			killed++
		}
	}
	return killed
}

func isOwnedMtgCommand(cmdline []byte, binaryPath string) bool {
	args := bytes.Split(bytes.TrimSuffix(cmdline, []byte{0}), []byte{0})
	if len(args) != 3 || string(args[1]) != "run" {
		return false
	}
	configPath := filepath.Clean(string(args[2]))
	configDir := filepath.Join(filepath.Dir(binaryPath), "mtproto")
	if filepath.Dir(configPath) != configDir {
		return false
	}
	name := filepath.Base(configPath)
	if !strings.HasPrefix(name, "mtg-") || !strings.HasSuffix(name, ".toml") {
		return false
	}
	id, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(name, "mtg-"), ".toml"))
	return err == nil && id > 0
}
