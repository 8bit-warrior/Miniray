package database

import (
	"path/filepath"
	"testing"

	"github.com/8bit-warrior/Miniray/internal/database/model"
)

func initMigrateDB(t *testing.T) {
	t.Helper()
	if err := InitDB(filepath.Join(t.TempDir(), "miniray.db")); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(func() { _ = CloseDB() })
}

func seedInboundWithStream(t *testing.T, tag string, port int, stream string) *model.Inbound {
	t.Helper()
	ib := &model.Inbound{
		UserId: 1, Tag: tag, Enable: true, Port: port, Protocol: model.VLESS,
		Remark: tag, Settings: `{"clients":[]}`, StreamSettings: stream,
	}
	if err := GetDB().Create(ib).Error; err != nil {
		t.Fatalf("create inbound %s: %v", tag, err)
	}
	return ib
}
