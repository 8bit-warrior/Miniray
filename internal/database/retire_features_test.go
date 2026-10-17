package database

import (
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/8bit-warrior/Miniray/internal/database/model"
	"github.com/8bit-warrior/Miniray/internal/xray"
)

func TestUpgradeRetiresRemoteRuntimeWithoutLosingLocalClients(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "miniray.db")
	if err := InitDB(dbPath); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = CloseDB() })
	queries := []string{
		`ALTER TABLE inbounds ADD COLUMN node_id INTEGER`,
		`ALTER TABLE inbounds ADD COLUMN origin_node_guid TEXT`,
		`CREATE INDEX idx_inbounds_node_id ON inbounds(node_id)`,
		`ALTER TABLE clients ADD COLUMN group_name TEXT`,
		`CREATE INDEX idx_clients_group_name ON clients(group_name)`,
		`ALTER TABLE clients ADD COLUMN sync_orphaned_at INTEGER`,
		`CREATE TABLE nodes(id INTEGER PRIMARY KEY, address TEXT)`,
		`CREATE TABLE hosts(id INTEGER PRIMARY KEY, inbound_id INTEGER, sort_order INTEGER, is_disabled BOOLEAN, address TEXT, port INTEGER, security TEXT, remark TEXT)`,
		`CREATE TABLE client_groups(id INTEGER PRIMARY KEY, name TEXT)`,
		`CREATE TABLE node_client_ips(id INTEGER PRIMARY KEY)`,
		`CREATE TABLE node_client_traffics(id INTEGER PRIMARY KEY)`,
		`CREATE TABLE node_pending_resets(id INTEGER PRIMARY KEY)`,
		`CREATE TABLE client_global_traffics(id INTEGER PRIMARY KEY)`,
		`INSERT INTO nodes VALUES(4,'remote.example.test')`,
		`INSERT INTO inbounds(id,user_id,tag,protocol,port,settings,stream_settings,enable,node_id) VALUES(10,1,'local','vless',24443,'{"clients":[]}','{"network":"ws","security":"tls"}',1,NULL),(11,1,'remote','vless',24443,'{"clients":[]}','{}',1,4)`,
		`INSERT INTO clients(id,email,uuid,enable,group_name) VALUES(7,'local@example.test','local-uuid',1,'retired-group')`,
		`INSERT INTO client_inbounds(client_id,inbound_id) VALUES(7,10),(7,11)`,
		`INSERT INTO client_traffics(email,inbound_id,enable,up,down,total) VALUES('local@example.test',10,1,123,456,9999)`,
		`INSERT INTO hosts VALUES(1,10,0,0,'cdn.example.test',8443,'tls','edge'),(2,11,0,0,'remote.example.test',443,'tls','remote'),(3,10,1,1,'disabled.example.test',8443,'tls','disabled')`,
		`INSERT INTO api_tokens(name,token,scope,enabled) VALUES('retired','obsolete-token','node-sync',1)`,
	}
	for _, query := range queries {
		if err := db.Exec(query).Error; err != nil {
			t.Fatalf("prepare old schema: %v", err)
		}
	}
	if err := InitDB(dbPath); err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	for _, table := range []string{"nodes", "hosts", "client_groups", "node_client_ips", "node_client_traffics", "node_pending_resets", "client_global_traffics"} {
		if db.Migrator().HasTable(table) {
			t.Errorf("retired table still exists: %s", table)
		}
	}
	for table, columns := range map[string][]string{"inbounds": {"node_id", "origin_node_guid"}, "clients": {"group_name", "sync_orphaned_at"}} {
		for _, column := range columns {
			if db.Migrator().HasColumn(table, column) {
				t.Errorf("retired column still exists: %s.%s", table, column)
			}
		}
	}
	var inbounds []model.Inbound
	if err := db.Find(&inbounds).Error; err != nil {
		t.Fatal(err)
	}
	if len(inbounds) != 1 || inbounds[0].Id != 10 {
		t.Fatalf("remote inbound became local: %+v", inbounds)
	}
	var stream struct {
		ExternalProxy []struct {
			Dest string
			Port int
		} `json:"externalProxy"`
	}
	if err := json.Unmarshal([]byte(inbounds[0].StreamSettings), &stream); err != nil {
		t.Fatal(err)
	}
	if len(stream.ExternalProxy) != 1 || stream.ExternalProxy[0].Dest != "cdn.example.test" || stream.ExternalProxy[0].Port != 8443 {
		t.Fatalf("lost local subscription endpoint: %+v", stream)
	}
	var client model.ClientRecord
	if err := db.First(&client, 7).Error; err != nil {
		t.Fatal(err)
	}
	if client.Email != "local@example.test" || client.UUID != "local-uuid" {
		t.Fatalf("local client changed: %+v", client)
	}
	var links []model.ClientInbound
	if err := db.Find(&links).Error; err != nil {
		t.Fatal(err)
	}
	if len(links) != 1 || links[0].InboundId != 10 {
		t.Fatalf("invalid retained associations: %+v", links)
	}
	var traffic xray.ClientTraffic
	if err := db.Where("email = ?", client.Email).First(&traffic).Error; err != nil {
		t.Fatal(err)
	}
	if traffic.Up != 123 || traffic.Down != 456 || traffic.Total != 9999 {
		t.Fatalf("local usage changed: %+v", traffic)
	}
	backups, err := filepath.Glob(filepath.Join(filepath.Dir(dbPath), "miniray-before-simplification-*.db"))
	if err != nil || len(backups) != 1 {
		t.Fatalf("backups = %v, error = %v", backups, err)
	}
	backup, err := sql.Open("sqlite3", backups[0]+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer backup.Close()
	var originalRemote int
	if err := backup.QueryRow("SELECT count(*) FROM inbounds WHERE node_id = 4").Scan(&originalRemote); err != nil {
		t.Fatal(err)
	}
	if originalRemote != 1 {
		t.Fatal("backup did not preserve the original remote configuration")
	}
	if err := InitDB(dbPath); err != nil {
		t.Fatalf("second open: %v", err)
	}
	backups, _ = filepath.Glob(filepath.Join(filepath.Dir(dbPath), "miniray-before-simplification-*.db"))
	if len(backups) != 1 {
		t.Fatalf("migration reran after completion: %v", backups)
	}
}
