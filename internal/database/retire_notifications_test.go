package database

import (
	"path/filepath"
	"testing"

	"github.com/8bit-warrior/Miniray/internal/database/model"
	"github.com/8bit-warrior/Miniray/internal/xray"
)

func TestUpgradeRemovesRetiredProtocolAndPreservesSharedClient(t *testing.T) {
	path := filepath.Join(t.TempDir(), "miniray.db")
	if err := InitDB(path); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = CloseDB() })
	for _, query := range []string{
		`ALTER TABLE clients ADD COLUMN tg_id INTEGER`,
		`CREATE INDEX idx_clients_tg_id ON clients(tg_id)`,
		`ALTER TABLE clients ADD COLUMN secret TEXT`,
		`ALTER TABLE clients ADD COLUMN ad_tag TEXT`,
		`INSERT INTO inbounds(id,user_id,tag,protocol,port,settings,stream_settings,enable) VALUES(10,1,'kept','vless',24443,'{"clients":[{"email":"shared","id":"uuid","tgId":123,"secret":"old","adTag":"old","enable":true}]}','{}',1),(11,1,'retired','mtproto',24444,'{"clients":[]}','{}',1)`,
		`INSERT INTO clients(id,email,uuid,enable,tg_id,secret,ad_tag) VALUES(7,'shared','uuid',1,123,'old','old')`,
		`INSERT INTO client_inbounds(client_id,inbound_id) VALUES(7,10),(7,11)`,
		`INSERT INTO client_traffics(email,inbound_id,enable,up,down,total) VALUES('shared',11,1,123,456,9999)`,
		`INSERT INTO settings(key,value) VALUES('tgBotToken','old'),('smtpPassword','old'),('discordBotToken','old')`,
	} {
		if err := db.Exec(query).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := InitDB(path); err != nil {
		t.Fatal(err)
	}
	var inbounds []model.Inbound
	if err := db.Find(&inbounds).Error; err != nil {
		t.Fatal(err)
	}
	if len(inbounds) != 1 || inbounds[0].Id != 10 {
		t.Fatalf("retained inbounds: %+v", inbounds)
	}
	var links []model.ClientInbound
	if err := db.Find(&links).Error; err != nil {
		t.Fatal(err)
	}
	if len(links) != 1 || links[0].InboundId != 10 || links[0].ClientId != 7 {
		t.Fatalf("retained links: %+v", links)
	}
	var client model.ClientRecord
	if err := db.First(&client, 7).Error; err != nil {
		t.Fatal(err)
	}
	if client.Email != "shared" || client.UUID != "uuid" {
		t.Fatalf("client identity changed: %+v", client)
	}
	var traffic xray.ClientTraffic
	if err := db.Where("email = ?", "shared").First(&traffic).Error; err != nil {
		t.Fatal(err)
	}
	if traffic.InboundId != 10 || traffic.Up != 123 || traffic.Down != 456 || traffic.Total != 9999 {
		t.Fatalf("traffic changed: %+v", traffic)
	}
	for _, column := range []string{"tg_id", "secret", "ad_tag"} {
		if db.Migrator().HasColumn("clients", column) {
			t.Errorf("retired column %s survived", column)
		}
	}
	var count int64
	if err := db.Model(&model.Setting{}).Where("key IN ?", []string{"tgBotToken", "smtpPassword", "discordBotToken"}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("retained %d notification settings", count)
	}
	if err := InitDB(path); err != nil {
		t.Fatalf("second open: %v", err)
	}
}
