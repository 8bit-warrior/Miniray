package database

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/8bit-warrior/Miniray/internal/database/model"
)

func TestStartupPreservesIPLimitsWithoutFail2ban(t *testing.T) {
	path := filepath.Join(t.TempDir(), "miniray.db")
	t.Setenv("MINIRAY_ENABLE_FAIL2BAN", "false")
	if err := InitDB(path); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = CloseDB() })
	inbound := model.Inbound{Tag: "limited", Protocol: model.VLESS, Port: 24443, Settings: `{"clients":[{"email":"limited@example.test","limitIp":6}]}`}
	record := model.ClientRecord{Email: "limited@example.test", LimitIP: 2}
	if err := db.Create(&inbound).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&record).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Where("seeder_name = ?", "ResetIpLimitNoFail2ban").Delete(&model.HistoryOfSeeders{}).Error; err != nil {
		t.Fatal(err)
	}
	if err := InitDB(path); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&inbound, inbound.Id).Error; err != nil {
		t.Fatal(err)
	}
	var settings struct {
		Clients []struct {
			LimitIP int `json:"limitIp"`
		} `json:"clients"`
	}
	if err := json.Unmarshal([]byte(inbound.Settings), &settings); err != nil {
		t.Fatal(err)
	}
	if len(settings.Clients) != 1 || settings.Clients[0].LimitIP != 6 {
		t.Fatalf("startup changed embedded limits: %s", inbound.Settings)
	}
	if err := db.First(&record, record.Id).Error; err != nil {
		t.Fatal(err)
	}
	if record.LimitIP != 2 {
		t.Fatalf("startup changed client IP limit: got %d, want 2", record.LimitIP)
	}
}
