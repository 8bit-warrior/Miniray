package database

import (
	"path/filepath"
	"testing"

	"github.com/8bit-warrior/Miniray/internal/database/model"

	"gorm.io/gorm"
)

func assertClientHwidSchema(t *testing.T, db *gorm.DB) {
	t.Helper()
	if !db.Migrator().HasColumn(&model.ClientRecord{}, "limit_hwid") {
		t.Fatalf("clients.limit_hwid missing")
	}
	if !db.Migrator().HasTable(&model.ClientHwid{}) {
		t.Fatalf("client_hwids table missing")
	}
	for _, col := range []string{"sub_id", "hwid_hash", "first_seen", "last_seen", "user_agent", "device_os", "os_version", "device_model"} {
		if !db.Migrator().HasColumn(&model.ClientHwid{}, col) {
			t.Fatalf("client_hwids.%s missing", col)
		}
	}
	if !db.Migrator().HasIndex(&model.ClientHwid{}, "idx_client_hwids_sub_hash") {
		t.Fatalf("client_hwids unique hash index missing")
	}
}

func TestClientHwidSchemaSQLite(t *testing.T) {
	dbDir := t.TempDir()
	t.Setenv("MINIRAY_DB_FOLDER", dbDir)
	if err := InitDB(filepath.Join(dbDir, "miniray.db")); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(func() { _ = CloseDB() })
	assertClientHwidSchema(t, GetDB())
}
