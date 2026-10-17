//go:build linux

package xray

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestVerifyCorePIDAfterExecutableReplacement(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MINIRAY_BIN_FOLDER", dir)
	binary := GetBinaryPath()
	original, err := os.ReadFile("/bin/sleep")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binary, original, 0o700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binary, "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()
	if err := os.Remove(binary); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binary, original, 0o700); err != nil {
		t.Fatal(err)
	}
	exe, err := os.Readlink(filepath.Join("/proc", strconv.Itoa(cmd.Process.Pid), "exe"))
	if err != nil || !strings.HasSuffix(exe, " (deleted)") {
		t.Fatalf("expected replaced executable; exe=%q err=%v", exe, err)
	}
	if err := verifyCorePID(cmd.Process.Pid); err != nil {
		t.Fatalf("managed PID rejected after replacement: %v", err)
	}
	if err := verifyCorePID(os.Getpid()); err == nil || !strings.Contains(err.Error(), "refusing to manage unrelated core PID") {
		t.Fatalf("unrelated PID accepted: %v", err)
	}
}

func TestVerifyCorePIDWithRelativeBinaryFolder(t *testing.T) {
	dir := t.TempDir()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	relativeDir, err := filepath.Rel(wd, dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("MINIRAY_BIN_FOLDER", relativeDir)
	binary := filepath.Join(dir, GetBinaryName())
	original, err := os.ReadFile("/bin/sleep")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binary, original, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(GetBinaryName(), filepath.Join(dir, "xray")); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(filepath.Join(dir, "xray"), "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()
	if err := verifyCorePID(cmd.Process.Pid); err != nil {
		t.Fatalf("core launched through symlink rejected with relative bin folder: %v", err)
	}
}

func TestManualCoreStopMarker(t *testing.T) {
	t.Setenv("MINIRAY_SEPARATE_CORE", "1")
	t.Setenv("MINIRAY_CORE_STOP_FILE", filepath.Join(t.TempDir(), "manual-stop"))
	if IndependentCoreManuallyStopped() {
		t.Fatal("core reported manually stopped before marker exists")
	}
	if err := SetIndependentCoreManuallyStopped(true); err != nil {
		t.Fatal(err)
	}
	if !IndependentCoreManuallyStopped() {
		t.Fatal("core stop marker was not observed")
	}
	if err := SetIndependentCoreManuallyStopped(false); err != nil {
		t.Fatal(err)
	}
	if IndependentCoreManuallyStopped() {
		t.Fatal("core remained manually stopped after clearing marker")
	}
}
