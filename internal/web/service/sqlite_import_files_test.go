package service

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMoveSQLiteFilesPreservesCompanions(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "source.db")
	dst := filepath.Join(dir, "fallback.db")
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if err := os.WriteFile(src+suffix, []byte("content"+suffix), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := moveSQLiteFiles(src, dst); err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"", "-wal", "-shm"} {
		data, err := os.ReadFile(dst + suffix)
		if err != nil || string(data) != "content"+suffix {
			t.Fatalf("moved %s: data %q, error %v", suffix, data, err)
		}
		if _, err := os.Stat(src + suffix); !os.IsNotExist(err) {
			t.Fatalf("source %s still exists: %v", suffix, err)
		}
	}
	if err := removeSQLiteFiles(dst); err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if _, err := os.Stat(dst + suffix); !os.IsNotExist(err) {
			t.Fatalf("cleanup %s: %v", suffix, err)
		}
	}
}

func TestMoveSQLiteFilesRollsBackWhenSidecarMoveFails(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "source.db")
	dst := filepath.Join(dir, "fallback.db")
	if err := os.WriteFile(src, []byte("database"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src+"-wal", []byte("journal"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(dst+"-wal", 0o700); err != nil {
		t.Fatal(err)
	}
	if err := moveSQLiteFiles(src, dst); err == nil {
		t.Fatal("expected sidecar move to fail")
	}
	for _, suffix := range []string{"", "-wal"} {
		if _, err := os.Stat(src + suffix); err != nil {
			t.Fatalf("source %s lost after failed move: %v", suffix, err)
		}
	}
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Fatalf("partially moved destination: %v", err)
	}
}
