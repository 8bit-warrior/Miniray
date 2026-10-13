package database

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/8bit-warrior/Miniray/internal/database/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// TestDumpAndRestoreSQLiteRoundTrip dumps a seeded SQLite db to .dump text and
// rebuilds it, asserting the row survives.
func TestDumpAndRestoreSQLiteRoundTrip(t *testing.T) {
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "src.db")
	dumpPath := filepath.Join(dir, "out.dump")
	dstPath := filepath.Join(dir, "rebuilt.db")

	src, err := gorm.Open(sqlite.Open(srcPath), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatalf("open src: %v", err)
	}
	if err := src.AutoMigrate(&model.Setting{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	if err := src.Create(&model.Setting{Key: "secret", Value: "o'brien \"quote\""}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	if sqlDB, _ := src.DB(); sqlDB != nil {
		sqlDB.Close()
	}

	if err := DumpSQLite(srcPath, dumpPath); err != nil {
		t.Fatalf("DumpSQLite: %v", err)
	}
	if fi, err := os.Stat(dumpPath); err != nil || fi.Size() == 0 {
		t.Fatalf("dump missing/empty: %v", err)
	}
	if err := RestoreSQLite(dumpPath, dstPath); err != nil {
		t.Fatalf("RestoreSQLite: %v", err)
	}

	dst, err := gorm.Open(sqlite.Open(dstPath), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatalf("open dst: %v", err)
	}
	defer closeGorm(dst)
	var s model.Setting
	if err := dst.Where("key = ?", "secret").First(&s).Error; err != nil {
		t.Fatalf("read back: %v", err)
	}
	if s.Value != "o'brien \"quote\"" {
		t.Errorf("value mismatch after round-trip: %q", s.Value)
	}
}

// closeGorm closes the underlying *sql.DB so Windows can delete the temp file.
func closeGorm(db *gorm.DB) {
	if db == nil {
		return
	}
	if s, err := db.DB(); err == nil {
		s.Close()
	}
}

func TestRestoreSQLiteWritesNoFileOutsideDestination(t *testing.T) {
	cases := []struct {
		name      string
		statement string
	}{
		{"attach", "ATTACH DATABASE '%s' AS x; CREATE TABLE x.t (a); INSERT INTO x.t VALUES (1);"},
		{"vacuum into", "CREATE TABLE t (a); VACUUM INTO '%s';"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			outside := filepath.Join(t.TempDir(), "outside.db")
			dumpPath := filepath.Join(dir, "in.dump")
			script := "PRAGMA foreign_keys=OFF;\n" + fmt.Sprintf(tc.statement, outside) + "\n"
			if err := os.WriteFile(dumpPath, []byte(script), 0o600); err != nil {
				t.Fatalf("write dump: %v", err)
			}

			err := RestoreSQLite(dumpPath, filepath.Join(dir, "rebuilt.db"))
			if err == nil {
				t.Fatal("RestoreSQLite accepted a dump that writes another database file")
			}
			if !strings.Contains(err.Error(), "too many attached databases") {
				t.Fatalf("restore failed for another reason: %v", err)
			}
			if _, statErr := os.Stat(outside); !os.IsNotExist(statErr) {
				t.Fatalf("restore created %s (stat err %v)", outside, statErr)
			}
		})
	}
}
