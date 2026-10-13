package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/8bit-warrior/Miniray/internal/database/model"
	"github.com/8bit-warrior/Miniray/internal/util/crypto"
	"github.com/8bit-warrior/Miniray/internal/xray"
	"github.com/mattn/go-sqlite3"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type LegacyImportResult struct {
	Inbounds                   int
	Clients                    int
	TrafficRows                int
	Settings                   int
	SkippedRemoteInbounds      int
	SkippedUnsupportedInbounds int
	ImportedAdmin              bool
}

var legacySettingKeys = map[string]bool{
	"xrayTemplateConfig": true, "webListen": true, "webDomain": true,
	"webPort": true, "webBasePath": true, "webCertFile": true, "webKeyFile": true,
	"sessionMaxAge": true, "trustedProxyCIDRs": true, "pageSize": true,
	"timeLocation": true, "panelOutbound": true,
	"subEnable": true, "subListen": true, "subPort": true, "subPath": true,
	"subDomain": true, "subCertFile": true, "subKeyFile": true, "subURI": true,
	"subJsonEnable": true, "subJsonPath": true, "subJsonURI": true,
	"subClashEnable": true, "subClashPath": true, "subClashURI": true,
	"subJsonAutoDetect": true, "subJsonAlwaysArray": true,
	"subJsonUserAgentRegex": true, "subClashAutoDetect": true,
	"subClashUserAgentRegex": true, "subTitle": true, "subAnnounce": true,
	"subSupportUrl": true, "subUpdates": true, "subEncrypt": true,
	"subEnableRouting": true, "subRoutingRules": true,
	"subJsonRules": true, "subJsonRoutingRules": true, "subJsonDns": true,
	"subClashEnableRouting": true, "subClashRules": true,
	"subJsonMux": true, "externalSubUserAgent": true,
	"tgBotEnable": true, "tgBotToken": true, "tgBotChatId": true,
	"tgRunTime": true, "tgBotProxy": true, "tgBotAPIServer": true,
	"remarkTemplate": true, "trafficDiff": true, "expireDiff": true,
	"subExpiredTemplate": true, "subTrafficDepletedTemplate": true,
}

func legacySQLiteDSN(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return (&url.URL{Scheme: "file", Path: filepath.ToSlash(absolute)}).String() + "?mode=ro&_busy_timeout=10000", nil
}

func openLegacySQLite(path string) (*gorm.DB, *sql.DB, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	valid, err := IsSQLiteDB(f)
	_ = f.Close()
	if err != nil {
		return nil, nil, err
	}
	if !valid {
		return nil, nil, errors.New("source is not a SQLite database")
	}
	dsn, err := legacySQLiteDSN(path)
	if err != nil {
		return nil, nil, err
	}
	raw, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, nil, err
	}
	source, err := gorm.Open(sqlite.New(sqlite.Config{Conn: raw}), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		_ = raw.Close()
		return nil, nil, err
	}
	var integrity string
	if err := raw.QueryRow("PRAGMA integrity_check").Scan(&integrity); err != nil {
		_ = raw.Close()
		return nil, nil, err
	}
	if integrity != "ok" {
		_ = raw.Close()
		return nil, nil, fmt.Errorf("legacy SQLite integrity_check: %s", integrity)
	}
	if !source.Migrator().HasTable("inbounds") || !source.Migrator().HasTable("settings") {
		_ = raw.Close()
		return nil, nil, errors.New("legacy database must contain inbounds and settings tables")
	}
	return source, raw, nil
}

// SnapshotLegacySQLite creates a transaction-consistent snapshot, including WAL data.
func SnapshotLegacySQLite(srcPath, dstPath string) (err error) {
	_, source, err := openLegacySQLite(srcPath)
	if err != nil {
		return err
	}
	defer source.Close()
	if _, err := os.Lstat(dstPath); err == nil {
		return fmt.Errorf("snapshot destination exists: %s", dstPath)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	defer func() {
		if err != nil {
			_ = os.Remove(dstPath)
		}
	}()
	target, err := sql.Open("sqlite3", dstPath)
	if err != nil {
		return err
	}
	defer target.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	srcConn, err := source.Conn(ctx)
	if err != nil {
		return err
	}
	defer srcConn.Close()
	dstConn, err := target.Conn(ctx)
	if err != nil {
		return err
	}
	defer dstConn.Close()
	err = srcConn.Raw(func(srcDriver any) error {
		src, ok := srcDriver.(*sqlite3.SQLiteConn)
		if !ok {
			return fmt.Errorf("unexpected source SQLite connection: %T", srcDriver)
		}
		return dstConn.Raw(func(dstDriver any) error {
			dst, ok := dstDriver.(*sqlite3.SQLiteConn)
			if !ok {
				return fmt.Errorf("unexpected destination SQLite connection: %T", dstDriver)
			}
			backup, err := dst.Backup("main", src, "main")
			if err != nil {
				return err
			}
			completed := false
			defer func() {
				if !completed {
					_ = backup.Finish()
				}
			}()
			for {
				done, stepErr := backup.Step(128)
				if stepErr != nil {
					return stepErr
				}
				if done {
					completed = true
					return backup.Finish()
				}
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(10 * time.Millisecond):
				}
			}
		})
	})
	if err != nil {
		return err
	}
	return os.Chmod(dstPath, 0o600)
}

func isSupportedLegacyInbound(p model.Protocol) bool {
	switch p {
	case model.VMESS, model.VLESS, model.Trojan, model.Shadowsocks,
		model.HTTP, model.Mixed, model.WireGuard, model.Hysteria,
		model.Tunnel, model.MTProto, model.AmneziaWG, model.TUIC:
		return true
	}
	return false
}

func dropLegacyGroupFields(settings string) (string, error) {
	if strings.TrimSpace(settings) == "" {
		return settings, nil
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(settings), &obj); err != nil {
		return "", err
	}
	if clients, ok := obj["clients"].([]any); ok {
		for _, raw := range clients {
			if client, ok := raw.(map[string]any); ok {
				delete(client, "group")
				delete(client, "groupName")
				delete(client, "group_name")
			}
		}
	}
	serialized, err := json.Marshal(obj)
	return string(serialized), err
}

// ImportLegacySQLite imports only supported local proxy data into a new Miniray database.
// The destination must not exist; no legacy database is modified.
func ImportLegacySQLite(srcPath, dstPath, legacyAssets string) (result LegacyImportResult, err error) {
	srcAbs, err := filepath.Abs(srcPath)
	if err != nil {
		return result, err
	}
	dstAbs, err := filepath.Abs(dstPath)
	if err != nil {
		return result, err
	}
	if srcAbs == dstAbs {
		return result, errors.New("source and destination must differ")
	}
	if _, err := os.Lstat(dstPath); err == nil {
		return result, fmt.Errorf("destination database exists: %s", dstPath)
	} else if !errors.Is(err, os.ErrNotExist) {
		return result, err
	}

	source, raw, err := openLegacySQLite(srcPath)
	if err != nil {
		return result, err
	}
	defer raw.Close()

	var inbounds []model.Inbound
	if err := source.Find(&inbounds).Error; err != nil {
		return result, err
	}
	var settings []model.Setting
	if err := source.Find(&settings).Error; err != nil {
		return result, err
	}
	var users []model.User
	if source.Migrator().HasTable("users") {
		if err := source.Order("id").Limit(1).Find(&users).Error; err != nil {
			return result, err
		}
	}
	var clients []model.ClientRecord
	if source.Migrator().HasTable("clients") {
		if err := source.Find(&clients).Error; err != nil {
			return result, err
		}
	}
	var joins []model.ClientInbound
	if source.Migrator().HasTable("client_inbounds") {
		if err := source.Find(&joins).Error; err != nil {
			return result, err
		}
	}
	var traffic []xray.ClientTraffic
	if source.Migrator().HasTable("client_traffics") {
		if err := source.Find(&traffic).Error; err != nil {
			return result, err
		}
	}
	var fallbacks []model.InboundFallback
	if source.Migrator().HasTable("inbound_fallbacks") {
		if err := source.Find(&fallbacks).Error; err != nil {
			return result, err
		}
	}
	var clientIPs []model.InboundClientIps
	if source.Migrator().HasTable("inbound_client_ips") {
		if err := source.Find(&clientIPs).Error; err != nil {
			return result, err
		}
	}
	var clientHWIDs []model.ClientHwid
	if source.Migrator().HasTable("client_hwids") {
		if err := source.Find(&clientHWIDs).Error; err != nil {
			return result, err
		}
	}
	var externalLinks []model.ClientExternalLink
	if source.Migrator().HasTable("client_external_links") {
		if err := source.Find(&externalLinks).Error; err != nil {
			return result, err
		}
	}
	var outboundSubs []model.OutboundSubscription
	if source.Migrator().HasTable("outbound_subscriptions") {
		if err := source.Find(&outboundSubs).Error; err != nil {
			return result, err
		}
	}

	defer func() {
		if err != nil {
			_ = CloseDB()
			db = nil
			for _, suffix := range []string{"", "-wal", "-shm"} {
				_ = os.Remove(dstPath + suffix)
			}
			_ = os.Remove(filepath.Join(filepath.Dir(dstPath), "initial-admin-password"))
		}
	}()
	if err := InitDB(dstPath); err != nil {
		return result, err
	}

	importedInbound := map[int]bool{}
	linkedSourceClients := map[int]bool{}
	importedSourceClients := map[int]bool{}
	importedEmails := map[string]bool{}
	importedSubIDs := map[string]bool{}
	rebase := func(value string) string {
		if legacyAssets == "" {
			return value
		}
		return strings.ReplaceAll(value, "/etc/x-ui/", strings.TrimSuffix(legacyAssets, "/")+"/")
	}
	err = db.Transaction(func(tx *gorm.DB) error {
		var admin model.User
		if err := tx.Order("id").First(&admin).Error; err != nil {
			return err
		}
		if len(users) != 0 && strings.TrimSpace(users[0].Username) != "" && users[0].Password != "" {
			password := users[0].Password
			if !crypto.IsHashed(password) {
				var hashErr error
				password, hashErr = crypto.HashPasswordAsBcrypt(password)
				if hashErr != nil {
					return hashErr
				}
			}
			if err := tx.Model(&admin).Updates(map[string]any{"username": users[0].Username, "password": password, "login_epoch": 0}).Error; err != nil {
				return err
			}
			result.ImportedAdmin = true
		}
		for _, item := range settings {
			if !legacySettingKeys[item.Key] {
				continue
			}
			if item.Key == "xrayTemplateConfig" && !json.Valid([]byte(item.Value)) {
				return errors.New("legacy Xray template is not valid JSON")
			}
			if err := tx.Where("key = ?", item.Key).Assign(map[string]any{"value": rebase(item.Value)}).FirstOrCreate(&model.Setting{Key: item.Key}).Error; err != nil {
				return err
			}
			result.Settings++
		}
		for _, src := range inbounds {
			if src.NodeID != nil || src.OriginNodeGuid != "" {
				result.SkippedRemoteInbounds++
				continue
			}
			if !isSupportedLegacyInbound(src.Protocol) {
				result.SkippedUnsupportedInbounds++
				continue
			}
			clean, err := dropLegacyGroupFields(rebase(src.Settings))
			if err != nil {
				return fmt.Errorf("inbound %d settings: %w", src.Id, err)
			}
			copy := model.Inbound{
				Id: src.Id, UserId: admin.Id, Up: src.Up, Down: src.Down,
				Total: src.Total, Remark: src.Remark, SubSortIndex: src.SubSortIndex,
				ExcludeFromSub: src.ExcludeFromSub, Enable: src.Enable,
				ExpiryTime: src.ExpiryTime, TrafficReset: src.TrafficReset,
				TrafficResetDay: src.TrafficResetDay, LastTrafficResetTime: src.LastTrafficResetTime,
				Listen: src.Listen, Port: src.Port, Protocol: src.Protocol, Settings: clean,
				StreamSettings: rebase(src.StreamSettings), Tag: src.Tag, Sniffing: src.Sniffing,
				ShareAddrStrategy: src.ShareAddrStrategy, ShareAddr: src.ShareAddr,
				DisableFlow: src.DisableFlow,
			}
			if err := tx.Create(&copy).Error; err != nil {
				return fmt.Errorf("import inbound %d: %w", src.Id, err)
			}
			importedInbound[src.Id] = true
			result.Inbounds++
		}
		for _, fallback := range fallbacks {
			if importedInbound[fallback.MasterId] && importedInbound[fallback.ChildId] {
				if err := tx.Create(&fallback).Error; err != nil {
					return err
				}
			}
		}
		for _, link := range joins {
			if importedInbound[link.InboundId] {
				linkedSourceClients[link.ClientId] = true
			}
		}
		for _, client := range clients {
			if !linkedSourceClients[client.Id] {
				continue
			}
			client.Group = ""
			client.SyncOrphanedAt = 0
			if err := tx.Create(&client).Error; err != nil {
				return fmt.Errorf("import client %d: %w", client.Id, err)
			}
			result.Clients++
			importedSourceClients[client.Id] = true
			importedEmails[strings.ToLower(client.Email)] = true
			if client.SubID != "" {
				importedSubIDs[client.SubID] = true
			}
		}
		for _, link := range joins {
			if importedInbound[link.InboundId] && importedSourceClients[link.ClientId] {
				if err := tx.Create(&link).Error; err != nil {
					return err
				}
			}
		}
		for _, inbound := range inbounds {
			if !importedInbound[inbound.Id] || strings.TrimSpace(inbound.Settings) == "" {
				continue
			}
			var obj struct {
				Clients []json.RawMessage `json:"clients"`
			}
			if err := json.Unmarshal([]byte(inbound.Settings), &obj); err != nil {
				return err
			}
			for _, rawClient := range obj.Clients {
				var payload map[string]any
				if err := json.Unmarshal(rawClient, &payload); err != nil {
					return err
				}
				model.NormalizeLegacyClientFields(payload)
				data, err := json.Marshal(payload)
				if err != nil {
					return err
				}
				var client model.Client
				if err := json.Unmarshal(data, &client); err != nil {
					return err
				}
				if strings.TrimSpace(client.Email) == "" {
					continue
				}
				record := client.ToRecord()
				record.Group = ""
				var existing model.ClientRecord
				found := tx.Where("email = ?", record.Email).First(&existing)
				if found.Error != nil && !errors.Is(found.Error, gorm.ErrRecordNotFound) {
					return found.Error
				}
				if errors.Is(found.Error, gorm.ErrRecordNotFound) {
					if err := tx.Create(record).Error; err != nil {
						return err
					}
					existing = *record
					result.Clients++
				}
				link := model.ClientInbound{ClientId: existing.Id, InboundId: inbound.Id, FlowOverride: client.Flow}
				if err := tx.Where("client_id = ? AND inbound_id = ?", link.ClientId, link.InboundId).FirstOrCreate(&link).Error; err != nil {
					return err
				}
				importedEmails[strings.ToLower(record.Email)] = true
				if record.SubID != "" {
					importedSubIDs[record.SubID] = true
				}
			}
		}
		for _, entry := range clientIPs {
			if importedEmails[strings.ToLower(entry.ClientEmail)] {
				if err := tx.Create(&entry).Error; err != nil {
					return err
				}
			}
		}
		for _, entry := range clientHWIDs {
			if importedSubIDs[entry.SubID] {
				if err := tx.Create(&entry).Error; err != nil {
					return err
				}
			}
		}
		for _, entry := range externalLinks {
			if importedSourceClients[entry.ClientId] {
				if err := tx.Create(&entry).Error; err != nil {
					return err
				}
			}
		}
		for _, entry := range outboundSubs {
			if err := tx.Create(&entry).Error; err != nil {
				return err
			}
		}
		for _, stat := range traffic {
			if !importedInbound[stat.InboundId] || !importedEmails[strings.ToLower(stat.Email)] {
				continue
			}
			stat.Id = 0
			if err := tx.Create(&stat).Error; err != nil {
				return fmt.Errorf("import traffic for %s: %w", stat.Email, err)
			}
			result.TrafficRows++
		}
		return nil
	})
	if err != nil {
		return result, err
	}
	if result.ImportedAdmin {
		_ = os.Remove(filepath.Join(filepath.Dir(dstPath), "initial-admin-password"))
	}
	return result, nil
}
