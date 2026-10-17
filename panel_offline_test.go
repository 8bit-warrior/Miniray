package main

import (
 "os"
 "path/filepath"
 "strings"
 "testing"
)

func TestOfflineUsesOpenRCServiceManager(t *testing.T) {
 dir := t.TempDir()
 script := filepath.Join(dir, "rc-service")
 output := filepath.Join(dir, "call")
 if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf '%s\\n' \"$*\" > \"$MINIRAY_SERVICE_TEST_CALL\"\n"), 0o700); err != nil { t.Fatal(err) }
 t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
 t.Setenv("MINIRAY_SERVICE_TEST_CALL", output)
 t.Setenv("MINIRAY_SEPARATE_CORE", "1")
 t.Setenv("MINIRAY_SERVICE_MANAGER", "openrc")
 signals := make(chan os.Signal, 1)
 stopPanelThroughService(signals)
 data, err := os.ReadFile(output)
 if err != nil { t.Fatal(err) }
 if strings.TrimSpace(string(data)) != "miniray stop" { t.Fatalf("unexpected command: %q", data) }
 select { case sig := <-signals: t.Fatalf("unexpected fallback: %v", sig); default: }
}

func TestOfflineFallsBackWhenManagerFails(t *testing.T) {
 dir := t.TempDir()
 if err := os.WriteFile(filepath.Join(dir, "rc-service"), []byte("#!/bin/sh\nexit 1\n"), 0o700); err != nil { t.Fatal(err) }
 t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
 t.Setenv("MINIRAY_SEPARATE_CORE", "1")
 t.Setenv("MINIRAY_SERVICE_MANAGER", "openrc")
 signals := make(chan os.Signal, 1)
 stopPanelThroughService(signals)
 select { case <-signals: default: t.Fatal("missing shutdown fallback") }
}
