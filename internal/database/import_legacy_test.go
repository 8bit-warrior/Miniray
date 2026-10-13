package database

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/8bit-warrior/Miniray/internal/database/model"
	"github.com/8bit-warrior/Miniray/internal/util/crypto"
	"github.com/8bit-warrior/Miniray/internal/xray"
)

func legacyImportFixture(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "old.db")
	raw, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	statements := []string{
		`CREATE TABLE users (id INTEGER PRIMARY KEY, username TEXT, password TEXT)`,
		`CREATE TABLE settings (id INTEGER PRIMARY KEY, key TEXT, value TEXT)`,
		`CREATE TABLE inbounds (id INTEGER PRIMARY KEY, user_id INTEGER, remark TEXT, tag TEXT, protocol TEXT, settings TEXT, stream_settings TEXT, sniffing TEXT, listen TEXT, port INTEGER, enable INTEGER, node_id INTEGER, origin_node_guid TEXT, total INTEGER)`,
		`CREATE TABLE clients (id INTEGER PRIMARY KEY, email TEXT, uuid TEXT, sub_id TEXT, password TEXT, group_name TEXT, enable INTEGER, total_gb INTEGER)`,
		`CREATE TABLE client_inbounds (client_id INTEGER, inbound_id INTEGER, flow_override TEXT)`,
		`CREATE TABLE client_traffics (id INTEGER PRIMARY KEY, inbound_id INTEGER, email TEXT, enable INTEGER, up INTEGER, down INTEGER, total INTEGER, expiry_time INTEGER)`,
		`CREATE TABLE inbound_fallbacks (id INTEGER PRIMARY KEY, master_id INTEGER, child_id INTEGER, path TEXT)`,
		`CREATE TABLE inbound_client_ips (id INTEGER PRIMARY KEY, client_email TEXT, ips TEXT)`,
		`CREATE TABLE client_hwids (id INTEGER PRIMARY KEY, sub_id TEXT, hwid_hash TEXT, first_seen INTEGER, last_seen INTEGER)`,
		`CREATE TABLE client_external_links (id INTEGER PRIMARY KEY, client_id INTEGER, kind TEXT, value TEXT, enable INTEGER)`,
		`CREATE TABLE outbound_subscriptions (id INTEGER PRIMARY KEY, remark TEXT, url TEXT, enabled INTEGER)`,
	}
	for _, query := range statements {
		if _, err := raw.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	hash, err := crypto.HashPasswordAsBcrypt("old-secret")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`INSERT INTO users (id,username,password) VALUES (5,'old-admin',?)`, hash); err != nil {
		t.Fatal(err)
	}
	for _, row := range [][2]string{
		{"webPort", "18443"}, {"webCertFile", "/etc/x-ui/certs/fullchain.pem"},
		{"subPath", "/old-subs/"}, {"apiToken", "unwanted-secret"},
		{"xrayTemplateConfig", `{"log":{"loglevel":"warning"}}`},
		{"obsoleteNodeSetting", "must-not-import"},
	} {
		if _, err := raw.Exec(`INSERT INTO settings(key,value) VALUES (?,?)`, row[0], row[1]); err != nil {
			t.Fatal(err)
		}
	}
	clientSettings := `{"clients":[{"id":"550e8400-e29b-41d4-a716-446655440000","email":"alice@example.com","group":"old-group","enable":true}]}`
	for _, row := range []struct {
		id                   int
		tag, proto, settings string
		node                 any
	}{
		{10, "local-one", "vless", clientSettings, nil},
		{11, "remote-node", "vless", clientSettings, 4},
		{12, "unsupported", "unknownproto", "{}", nil},
		{13, "local-two", "trojan", `{"clients":[{"id":"660e8400-e29b-41d4-a716-446655440001","email":"bob@example.com","enable":true}]}`, nil},
	} {
		if _, err := raw.Exec(`INSERT INTO inbounds(id,user_id,remark,tag,protocol,settings,stream_settings,sniffing,listen,port,enable,node_id,total) VALUES (?,1,'test',?,?,?,'{"security":"tls","cert":"/etc/x-ui/certs/fullchain.pem"}','{}','0.0.0.0',443,1,?,2048)`, row.id, row.tag, row.proto, row.settings, row.node); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := raw.Exec(`INSERT INTO clients(id,email,uuid,sub_id,password,group_name,enable,total_gb) VALUES (7,'alice@example.com','alice-uuid','alice-sub','secret','old-group',1,4096),(8,'remote@example.com','remote-uuid','remote-sub','secret','old-group',1,1024)`); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`INSERT INTO client_inbounds(client_id,inbound_id) VALUES (7,10),(7,13),(8,11)`); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`INSERT INTO client_traffics(id,inbound_id,email,enable,up,down,total,expiry_time) VALUES (1,10,'alice@example.com',1,123,456,9999,123456),(2,11,'remote@example.com',1,1,1,9999,0),(3,13,'bob@example.com',1,5,9,9999,123456)`); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`INSERT INTO inbound_fallbacks(id,master_id,child_id,path) VALUES (1,10,13,'/cross'),(2,10,11,'/remote')`); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{
		`INSERT INTO inbound_client_ips(id,client_email,ips) VALUES (1,'alice@example.com','["1.2.3.4"]'),(2,'remote@example.com','["5.6.7.8"]')`,
		`INSERT INTO client_hwids(id,sub_id,hwid_hash,first_seen,last_seen) VALUES (1,'alice-sub','abcdef',100,200),(2,'remote-sub','123456',100,200)`,
		`INSERT INTO client_external_links(id,client_id,kind,value,enable) VALUES (1,7,'link','vless://sample',1),(2,8,'link','vless://remote',1)`,
		`INSERT INTO outbound_subscriptions(id,remark,url,enabled) VALUES (1,'upstream','https://example.com/sub',1)`,
	} {
		if _, err := raw.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

func TestLegacySQLiteSnapshotAndSelectiveImport(t *testing.T) {
	sourcePath := legacyImportFixture(t)
	snapshotPath := filepath.Join(t.TempDir(), "snapshot.db")
	if err := SnapshotLegacySQLite(sourcePath, snapshotPath); err != nil {
		t.Fatal(err)
	}
	if err := ValidateSQLiteDB(snapshotPath); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), "miniray.db")
	result, err := ImportLegacySQLite(snapshotPath, dst, "/etc/miniray/imported-legacy-files")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = CloseDB() })
	if result.Inbounds != 2 || result.Clients != 2 || result.TrafficRows != 2 || result.SkippedRemoteInbounds != 1 || result.SkippedUnsupportedInbounds != 1 || !result.ImportedAdmin {
		t.Fatalf("import result: %+v", result)
	}
	var inbounds []model.Inbound
	if err := GetDB().Order("id").Find(&inbounds).Error; err != nil {
		t.Fatal(err)
	}
	if len(inbounds) != 2 || inbounds[0].Id != 10 || inbounds[1].Id != 13 || inbounds[0].NodeID != nil {
		t.Fatalf("inbounds: %+v", inbounds)
	}
	var settings map[string]any
	if err := json.Unmarshal([]byte(inbounds[0].Settings), &settings); err != nil {
		t.Fatal(err)
	}
	embeddedClient := settings["clients"].([]any)[0].(map[string]any)
	if _, present := embeddedClient["group"]; present {
		t.Fatal("legacy group field was retained")
	}
	if inbounds[0].StreamSettings == "" || inbounds[0].StreamSettings == `{"security":"tls","cert":"/etc/x-ui/certs/fullchain.pem"}` {
		t.Fatal("certificate path was not rebased")
	}
	var client model.ClientRecord
	if err := GetDB().Where("email = ?", "alice@example.com").First(&client).Error; err != nil {
		t.Fatal(err)
	}
	if client.Group != "" || client.UUID != "alice-uuid" || client.TotalGB != 4096 {
		t.Fatalf("client not preserved correctly: %+v", client)
	}
	var links []model.ClientInbound
	if err := GetDB().Find(&links).Error; err != nil {
		t.Fatal(err)
	}
	if len(links) != 3 {
		t.Fatalf("client links: %+v", links)
	}
	var jsonOnlyClient model.ClientRecord
	if err := GetDB().Where("email = ?", "bob@example.com").First(&jsonOnlyClient).Error; err != nil {
		t.Fatal(err)
	}
	var traffic []xray.ClientTraffic
	if err := GetDB().Find(&traffic).Error; err != nil {
		t.Fatal(err)
	}
	if len(traffic) != 2 {
		t.Fatalf("traffic: %+v", traffic)
	}
	byInbound := map[int]xray.ClientTraffic{}
	for _, stat := range traffic {
		byInbound[stat.InboundId] = stat
	}
	if byInbound[10].Up != 123 || byInbound[10].Down != 456 || byInbound[13].Up != 5 || byInbound[13].Down != 9 || byInbound[11].Up != 0 {
		t.Fatalf("traffic: %+v", traffic)
	}
	var fallbacks []model.InboundFallback
	if err := GetDB().Find(&fallbacks).Error; err != nil {
		t.Fatal(err)
	}
	if len(fallbacks) != 1 || fallbacks[0].ChildId != 13 {
		t.Fatalf("fallbacks: %+v", fallbacks)
	}
	for _, tc := range []struct {
		name  string
		model any
	}{
		{"IPs", &[]model.InboundClientIps{}},
		{"HWIDs", &[]model.ClientHwid{}},
		{"external links", &[]model.ClientExternalLink{}},
		{"outbound subscriptions", &[]model.OutboundSubscription{}},
	} {
		var count int64
		if err := GetDB().Model(tc.model).Count(&count).Error; err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatalf("%s imported count = %d, want 1", tc.name, count)
		}
	}
	var importedSettings []model.Setting
	if err := GetDB().Where("key IN ?", []string{"webCertFile", "apiToken", "obsoleteNodeSetting"}).Find(&importedSettings).Error; err != nil {
		t.Fatal(err)
	}
	for _, setting := range importedSettings {
		if setting.Key == "apiToken" && setting.Value == "unwanted-secret" {
			t.Fatal("old token was imported")
		}
		if setting.Key == "obsoleteNodeSetting" {
			t.Fatal("removed setting was imported")
		}
		if setting.Key == "webCertFile" && setting.Value != "/etc/miniray/imported-legacy-files/certs/fullchain.pem" {
			t.Fatalf("cert path: %s", setting.Value)
		}
	}
	var user model.User
	if err := GetDB().Order("id").First(&user).Error; err != nil {
		t.Fatal(err)
	}
	if user.Username != "old-admin" || !crypto.CheckPasswordHash(user.Password, "old-secret") {
		t.Fatal("legacy admin credentials not preserved")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dst), "initial-admin-password")); !os.IsNotExist(err) {
		t.Fatalf("stale bootstrap credential should be removed: %v", err)
	}
	if _, err := ImportLegacySQLite(snapshotPath, dst, ""); err == nil {
		t.Fatal("import must refuse to overwrite an existing destination")
	}
}

func TestLegacyImportRollbackOnInvalidInbound(t *testing.T) {
	source := legacyImportFixture(t)
	db, err := sql.Open("sqlite3", source)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE inbounds SET tag = 'local-one' WHERE id = 13`); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	dst := filepath.Join(t.TempDir(), "miniray.db")
	if _, err := ImportLegacySQLite(source, dst, ""); err == nil {
		t.Fatal("duplicate tag should fail transaction")
	}
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Fatalf("failed migration left target database: %v", err)
	}
	if _, err := os.Stat(source); err != nil {
		t.Fatalf("legacy source was modified: %v", err)
	}
}
