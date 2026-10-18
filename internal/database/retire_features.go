package database

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/8bit-warrior/Miniray/internal/database/model"
)

// retireRemovedFeatures is the one-time conversion from the previous schema.
func retireRemovedFeatures(dbPath string) error {
	removedTables := []string{"nodes", "hosts", "client_groups", "node_client_ips", "node_client_traffics", "node_pending_resets", "client_global_traffics"}
	legacy := db.Migrator().HasColumn("inbounds", "node_id")
	for _, column := range []string{"tg_id", "secret", "ad_tag"} {
		legacy = legacy || db.Migrator().HasColumn("clients", column)
	}
	if db.Migrator().HasTable("inbounds") {
		var retired int64
		if err := db.Table("inbounds").Where(`protocol = ? OR settings LIKE ? OR settings LIKE ? OR settings LIKE ?`, "mtproto", `%"tgId"%`, `%"secret"%`, `%"adTag"%`).Count(&retired).Error; err != nil {
			return err
		}
		legacy = legacy || retired > 0
	}
	for _, table := range removedTables {
		legacy = legacy || db.Migrator().HasTable(table)
	}
	if !legacy {
		return nil
	}
	backup := filepath.Join(filepath.Dir(dbPath), "miniray-before-simplification-"+time.Now().UTC().Format("20060102T150405.000000000")+".db")
	if err := BackupSQLite(backup); err != nil {
		return fmt.Errorf("back up previous schema: %w", err)
	}
	return db.Transaction(func(tx *gorm.DB) error {
		if tx.Migrator().HasTable("inbounds") {
			retired := tx.Table("inbounds").Select("id").Where("protocol = ?", "mtproto")
			for table, column := range map[string]string{"client_inbounds": "inbound_id", "inbound_fallbacks": "master_id", "hosts": "inbound_id"} {
				if tx.Migrator().HasTable(table) {
					if err := tx.Table(table).Where(column+" IN (?)", retired).Delete(nil).Error; err != nil {
						return err
					}
				}
			}
			if tx.Migrator().HasTable("inbound_fallbacks") {
				if err := tx.Table("inbound_fallbacks").Where("child_id IN (?)", retired).Delete(nil).Error; err != nil {
					return err
				}
			}
			if tx.Migrator().HasTable("client_traffics") {
				replacement := "0"
				if tx.Migrator().HasTable("clients") && tx.Migrator().HasTable("client_inbounds") {
					replacement = `(SELECT MIN(ci.inbound_id) FROM client_inbounds ci
      JOIN clients c ON c.id = ci.client_id JOIN inbounds i ON i.id = ci.inbound_id
      WHERE c.email = client_traffics.email AND i.protocol <> 'mtproto')`
				}
				if err := tx.Exec("UPDATE client_traffics SET inbound_id = COALESCE("+replacement+", 0) WHERE inbound_id IN (?)", retired).Error; err != nil {
					return err
				}
			}
			if err := tx.Exec("DELETE FROM inbounds WHERE protocol = ?", "mtproto").Error; err != nil {
				return err
			}
			var inbounds []model.Inbound
			if err := tx.Find(&inbounds).Error; err != nil {
				return err
			}
			for _, inbound := range inbounds {
				if !strings.Contains(inbound.Settings, `"tgId"`) && !strings.Contains(inbound.Settings, `"adTag"`) && !strings.Contains(inbound.Settings, `"secret"`) {
					continue
				}
				clean, err := dropLegacyGroupFields(inbound.Settings)
				if err != nil {
					return err
				}
				if err := tx.Model(&model.Inbound{}).Where("id = ?", inbound.Id).Update("settings", clean).Error; err != nil {
					return err
				}
			}
		}
		if tx.Migrator().HasColumn("inbounds", "node_id") {
			remote := tx.Table("inbounds").Select("id").Where("node_id IS NOT NULL AND node_id <> 0")
			for table, column := range map[string]string{"client_inbounds": "inbound_id", "inbound_fallbacks": "master_id", "hosts": "inbound_id"} {
				if tx.Migrator().HasTable(table) {
					if err := tx.Table(table).Where(column+" IN (?)", remote).Delete(nil).Error; err != nil {
						return err
					}
				}
			}
			if tx.Migrator().HasTable("inbound_fallbacks") {
				if err := tx.Table("inbound_fallbacks").Where("child_id IN (?)", remote).Delete(nil).Error; err != nil {
					return err
				}
			}
			if err := tx.Exec("DELETE FROM inbounds WHERE node_id IS NOT NULL AND node_id <> 0").Error; err != nil {
				return err
			}
		}
		if err := preserveLegacyEndpoints(tx); err != nil {
			return err
		}
		for _, table := range removedTables {
			if err := tx.Exec(`DROP TABLE IF EXISTS "` + table + `"`).Error; err != nil {
				return err
			}
		}
		for table, columns := range map[string][]string{"inbounds": {"node_id", "origin_node_guid"}, "clients": {"group_name", "sync_orphaned_at", "tg_id", "secret", "ad_tag"}} {
			for _, column := range columns {
				if tx.Migrator().HasColumn(table, column) {
					if err := dropRetiredColumn(tx, table, column); err != nil {
						return err
					}
				}
			}
		}
		if tx.Migrator().HasTable("api_tokens") {
			if err := tx.Exec("DELETE FROM api_tokens WHERE scope = 'node-sync'").Error; err != nil {
				return err
			}
		}
		if tx.Migrator().HasTable("settings") {
			return tx.Exec("DELETE FROM settings WHERE key LIKE 'nodeMtls%' OR key = 'panelGuid'").Error
		}
		return nil
	})
}

func dropRetiredColumn(tx *gorm.DB, table, column string) error {
	var indexes []sqliteIndexListRow
	if err := tx.Raw(`PRAGMA index_list("` + table + `")`).Scan(&indexes).Error; err != nil {
		return err
	}
	for _, index := range indexes {
		var columns []struct{ Name string }
		if err := tx.Raw(`PRAGMA index_info("` + index.Name + `")`).Scan(&columns).Error; err != nil {
			return err
		}
		for _, col := range columns {
			if col.Name == column {
				if err := tx.Exec(`DROP INDEX "` + index.Name + `"`).Error; err != nil {
					return err
				}
				break
			}
		}
	}
	return tx.Exec(`ALTER TABLE "` + table + `" DROP COLUMN "` + column + `"`).Error
}

func preserveLegacyEndpoints(tx *gorm.DB) error {
	if !tx.Migrator().HasTable("hosts") {
		return nil
	}
	var rows []map[string]any
	if err := tx.Table("hosts").Order("sort_order, id").Find(&rows).Error; err != nil {
		return err
	}
	endpoints := map[int][]map[string]any{}
	for _, row := range rows {
		if row["is_disabled"] == true || row["is_disabled"] == int64(1) {
			continue
		}
		id, _ := row["inbound_id"].(int64)
		ep := map[string]any{"forceTls": "same", "endpointOverrides": true}
		for from, to := range map[string]string{
			"address": "dest", "port": "port", "remark": "remark", "sni": "sni",
			"host_header": "hostHeader", "path": "path", "fingerprint": "fingerprint",
			"cipher_suites": "cipherSuites", "verify_peer_cert_by_name": "verifyPeerCertByName",
			"ech_config_list": "echConfigList", "mux_params": "muxParams", "sockopt_params": "sockoptParams",
			"final_mask": "finalMask", "vless_route": "vlessRoute", "server_description": "serverDescription",
		} {
			if value, exists := row[from]; exists && value != nil {
				ep[to] = value
			}
		}
		if security, _ := row["security"].(string); security == "tls" || security == "none" {
			ep["forceTls"] = security
		}
		for from, to := range map[string]string{"alpn": "alpn", "pinned_peer_cert_sha256": "pinnedPeerCertSha256", "exclude_from_sub_types": "excludeFromSubTypes"} {
			if raw, _ := row[from].(string); raw != "" {
				var values []string
				if err := json.Unmarshal([]byte(raw), &values); err != nil {
					return fmt.Errorf("convert legacy endpoint %v: %w", row["id"], err)
				}
				ep[to] = values
			}
		}
		if row["allow_insecure"] == true || row["allow_insecure"] == int64(1) {
			ep["allowInsecure"] = true
		}
		if row["override_sni_from_address"] == true || row["override_sni_from_address"] == int64(1) {
			ep["sni"] = row["address"]
		}
		if row["keep_sni_blank"] == true || row["keep_sni_blank"] == int64(1) {
			delete(ep, "sni")
		}
		endpoints[int(id)] = append(endpoints[int(id)], ep)
	}
	for id, entries := range endpoints {
		var inbound model.Inbound
		if err := tx.First(&inbound, id).Error; err != nil {
			return err
		}
		stream := map[string]any{}
		if strings.TrimSpace(inbound.StreamSettings) != "" {
			if err := json.Unmarshal([]byte(inbound.StreamSettings), &stream); err != nil {
				return err
			}
		}
		for _, ep := range entries {
			if ep["dest"] == "" || ep["dest"] == nil {
				delete(ep, "dest")
			}
			if ep["port"] == int64(0) || ep["port"] == nil {
				ep["port"] = inbound.Port
			}
		}
		stream["externalProxy"] = entries
		encoded, err := json.Marshal(stream)
		if err != nil {
			return err
		}
		if err := tx.Model(&model.Inbound{}).Where("id = ?", id).Update("stream_settings", string(encoded)).Error; err != nil {
			return err
		}
	}
	return nil
}
