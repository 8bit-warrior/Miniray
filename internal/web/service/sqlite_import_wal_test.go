package service

import (
	"database/sql"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func TestMoveSQLiteFilesDoesNotReusePreviousWAL(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("SQLite WAL files cannot be renamed while in use on Windows")
	}
	dir := t.TempDir()
	livePath := filepath.Join(dir, "miniray.db")
	stagedPath := filepath.Join(dir, "staged.db")
	fallbackPath := filepath.Join(dir, "fallback.db")

	old, err := sql.Open("sqlite3", livePath)
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close()
	for _, query := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA wal_autocheckpoint=0",
		"CREATE TABLE marker(value TEXT)",
		"INSERT INTO marker(value) VALUES ('original')",
	} {
		if _, err := old.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(livePath + "-wal"); err != nil {
		t.Fatalf("expected live SQLite WAL: %v", err)
	}

	incoming, err := sql.Open("sqlite3", stagedPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{
		"CREATE TABLE marker(value TEXT)",
		"INSERT INTO marker(value) VALUES ('imported')",
	} {
		if _, err := incoming.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	if err := incoming.Close(); err != nil {
		t.Fatal(err)
	}

	if err := moveSQLiteFiles(livePath, fallbackPath); err != nil {
		t.Fatal(err)
	}
	if err := moveSQLiteFiles(stagedPath, livePath); err != nil {
		t.Fatal(err)
	}

	checkMarker := func(path, want string) {
		t.Helper()
		conn, err := sql.Open("sqlite3", path)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		var got string
		if err := conn.QueryRow("SELECT value FROM marker").Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("%s marker = %q, want %q", path, got, want)
		}
	}
	checkMarker(livePath, "imported")
	checkMarker(fallbackPath, "original")
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}
	checkMarker(livePath, "imported")
	if err := removeSQLiteFiles(livePath); err != nil {
		t.Fatal(err)
	}
	if err := moveSQLiteFiles(fallbackPath, livePath); err != nil {
		t.Fatal(err)
	}
	checkMarker(livePath, "original")
}
