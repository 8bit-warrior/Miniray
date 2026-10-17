package service

import (
	"path/filepath"
	"testing"

	"github.com/8bit-warrior/Miniray/internal/database"
	"github.com/8bit-warrior/Miniray/internal/database/dbtest"
	"github.com/8bit-warrior/Miniray/internal/database/model"
)

func TestSetInboundSubSortIndexLeavesSettingsUntouched(t *testing.T) {
	dbtest.InitDB(t, filepath.Join(t.TempDir(), "miniray.db"))

	const settings = `{"clients":[{"email":"a@example.test","id":"11111111-1111-1111-1111-111111111111"}]}`
	ib := &model.Inbound{UserId: 1, Remark: "r", Port: 21001, Protocol: model.VLESS, Settings: settings, SubSortIndex: 1, Enable: true}
	if err := database.GetDB().Create(ib).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}

	svc := InboundService{}
	if err := svc.SetInboundSubSortIndex(ib.Id, 7); err != nil {
		t.Fatalf("set: %v", err)
	}

	var got model.Inbound
	if err := database.GetDB().First(&got, ib.Id).Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got.SubSortIndex != 7 {
		t.Fatalf("subSortIndex = %d, want 7", got.SubSortIndex)
	}
	if got.Settings != settings {
		t.Fatalf("settings were rewritten:\n got %s\nwant %s", got.Settings, settings)
	}
}

func TestSetInboundSubSortIndexPreservesNegative(t *testing.T) {
	dbtest.InitDB(t, filepath.Join(t.TempDir(), "miniray.db"))

	ib := &model.Inbound{UserId: 1, Remark: "r", Port: 21003, Protocol: model.VLESS, Settings: `{"clients":[]}`, SubSortIndex: 1, Enable: true}
	if err := database.GetDB().Create(ib).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := (&InboundService{}).SetInboundSubSortIndex(ib.Id, -2); err != nil {
		t.Fatalf("set: %v", err)
	}

	var got model.Inbound
	if err := database.GetDB().First(&got, ib.Id).Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got.SubSortIndex != -2 {
		t.Fatalf("subSortIndex = %d, want -2", got.SubSortIndex)
	}
}
