package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	"github.com/8bit-warrior/Miniray/internal/database"
	"github.com/8bit-warrior/Miniray/internal/database/model"
	"github.com/8bit-warrior/Miniray/internal/logger"
	"github.com/8bit-warrior/Miniray/internal/xray"
)

func (s *InboundService) MigrationRemoveOrphanedTraffics() {
	db := database.GetDB()
	query := fmt.Sprintf(
		"DELETE FROM client_traffics WHERE email NOT IN (SELECT email FROM clients) AND email NOT IN (SELECT %s %s)",
		database.JSONFieldText("client.value", "email"),
		database.JSONClientsFromInbound(),
	)
	result := db.Exec(query)
	if result.Error != nil {
		logger.Warning("MigrationRemoveOrphanedTraffics failed:", result.Error)
		return
	}
	if result.RowsAffected > 0 {
		logger.Infof("MigrationRemoveOrphanedTraffics: removed %d orphaned client_traffics row(s)", result.RowsAffected)
	}
}

func (s *InboundService) MigrationRequirements() (err error) {
	db := database.GetDB()
	tx := db.Begin()
	defer func() {
		if err == nil {
			if commitErr := tx.Commit().Error; commitErr != nil {
				err = commitErr
				return
			}
			{
				if dbErr := db.Exec(`VACUUM "main"`).Error; dbErr != nil {
					logger.Warningf("VACUUM failed: %v", dbErr)
				}
			}
		} else {
			tx.Rollback()
		}
	}()

	if tx.Migrator().HasColumn(&model.Inbound{}, "all_time") {
		if err = tx.Migrator().DropColumn(&model.Inbound{}, "all_time"); err != nil {
			return
		}
	}
	if tx.Migrator().HasColumn(&xray.ClientTraffic{}, "all_time") {
		if err = tx.Migrator().DropColumn(&xray.ClientTraffic{}, "all_time"); err != nil {
			return
		}
	}
	if err = normalizeInboundShareAddressColumns(tx); err != nil {
		return
	}

	// Fix inbounds based problems
	var inbounds []*model.Inbound
	err = tx.Model(model.Inbound{}).Where("protocol IN (?)", []string{"vmess", "vless", "trojan", "shadowsocks", "hysteria"}).Find(&inbounds).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return
	}
	for inbound_index := range inbounds {
		settings := map[string]any{}
		_ = json.Unmarshal([]byte(inbounds[inbound_index].Settings), &settings)
		if raw, exists := settings["clients"]; exists && raw == nil {
			settings["clients"] = []any{}
		}
		clients, ok := settings["clients"].([]any)
		if ok {
			// Fix Client configuration problems
			newClients := make([]any, 0, len(clients))
			hasVisionFlow := false
			for client_index := range clients {
				c := clients[client_index].(map[string]any)

				// Add email='' if it is not exists
				if _, ok := c["email"]; !ok {
					c["email"] = ""
				}

				// Remove "flow": "xtls-rprx-direct"
				if _, ok := c["flow"]; ok {
					if c["flow"] == "xtls-rprx-direct" {
						c["flow"] = ""
					}
				}
				if flow, _ := c["flow"].(string); flow == "xtls-rprx-vision" {
					hasVisionFlow = true
				}
				// Backfill created_at and updated_at
				if _, ok := c["created_at"]; !ok {
					c["created_at"] = time.Now().Unix() * 1000
				}
				c["updated_at"] = time.Now().Unix() * 1000
				newClients = append(newClients, any(c))
			}
			settings["clients"] = newClients

			// Drop orphaned testseed: VLESS-only field, only meaningful when at least
			// one client uses the exact xtls-rprx-vision flow. Older versions saved it
			// for any non-empty flow (including the UDP variant) or kept it after the
			// flow was cleared from the client modal — clean those up here.
			if inbounds[inbound_index].Protocol == model.VLESS && !hasVisionFlow {
				delete(settings, "testseed")
			}

			var modifiedSettings []byte
			modifiedSettings, err = json.MarshalIndent(settings, "", "  ")
			if err != nil {
				return
			}

			inbounds[inbound_index].Settings = string(modifiedSettings)
		}

		// Add client traffic row for all clients which has email
		var modelClients []model.Client
		modelClients, err = s.GetClients(inbounds[inbound_index])
		if err != nil {
			return
		}
		for _, modelClient := range modelClients {
			if len(modelClient.Email) > 0 {
				var count int64
				if err = tx.Model(xray.ClientTraffic{}).Where("email = ?", modelClient.Email).Count(&count).Error; err != nil {
					return
				}
				if count == 0 {
					if err = s.AddClientStat(tx, inbounds[inbound_index].Id, &modelClient); err != nil {
						return
					}
				}
			}
		}

		// Synchronize client records after repairing inbound settings.
		if err = s.clientService.SyncInbound(tx, inbounds[inbound_index].Id, modelClients); err != nil {
			return
		}
	}
	if err = tx.Save(inbounds).Error; err != nil {
		return
	}

	// Remove orphaned traffics
	if err = tx.Where("inbound_id = 0").Delete(xray.ClientTraffic{}).Error; err != nil {
		return
	}

	// Migrate old MultiDomain to External Proxy
	var externalProxy []struct {
		Id             int
		Port           int
		StreamSettings string // text column on both DBs; safer than []byte for cross-DB scan
	}
	externalProxyQuery := `select id, port, stream_settings
	from inbounds
	WHERE protocol in ('vmess','vless','trojan')
	  AND json_extract(stream_settings, '$.security') = 'tls'
	  AND json_extract(stream_settings, '$.tlsSettings.settings.domains') IS NOT NULL`

	err = tx.Raw(externalProxyQuery).Scan(&externalProxy).Error
	if err != nil || len(externalProxy) == 0 {
		return
	}

	for _, ep := range externalProxy {
		var reverses any
		var stream map[string]any
		_ = json.Unmarshal([]byte(ep.StreamSettings), &stream)
		if tlsSettings, ok := stream["tlsSettings"].(map[string]any); ok {
			if settings, ok := tlsSettings["settings"].(map[string]any); ok {
				if domains, ok := settings["domains"].([]any); ok {
					for _, domain := range domains {
						if domainMap, ok := domain.(map[string]any); ok {
							domainMap["forceTls"] = "same"
							domainMap["port"] = ep.Port
							domainMap["dest"] = domainMap["domain"].(string)
							delete(domainMap, "domain")
						}
					}
				}
				reverses = settings["domains"]
				delete(settings, "domains")
			}
		}
		stream["externalProxy"] = reverses
		newStream, marshalErr := json.MarshalIndent(stream, " ", "  ")
		if marshalErr != nil {
			err = marshalErr
			return
		}
		if err = tx.Model(model.Inbound{}).Where("id = ?", ep.Id).Update("stream_settings", newStream).Error; err != nil {
			return
		}
	}

	tagCleanup := `UPDATE inbounds
		SET tag = REPLACE(tag, '0.0.0.0:', '')
		WHERE INSTR(tag, '0.0.0.0:') > 0
		  AND NOT EXISTS (
			SELECT 1 FROM inbounds AS other
			WHERE other.id <> inbounds.id
			  AND other.tag = REPLACE(inbounds.tag, '0.0.0.0:', '')
		  );`

	err = tx.Exec(tagCleanup).Error
	if err != nil {
		return
	}
	return err
}

func (s *InboundService) MigrateDB() {
	if err := s.MigrationRequirements(); err != nil {
		logger.Errorf("MigrationRequirements failed: %v", err)
	}
	s.MigrationRemoveOrphanedTraffics()
	s.MigrationRestoreVisionFlow()
}

// MigrationRestoreVisionFlow repairs VLESS inbounds whose clients lost their
// XTLS Vision flow because the inbound was not flow-eligible when the client was
// written (e.g. an XHTTP inbound whose vlessenc encryption was enabled only
// later). For each now-eligible inbound it restores flow=xtls-rprx-vision on
// clients whose intended flow (their flow_override on a sibling inbound) is
// Vision. Idempotent: once a client carries the flow it is skipped, so this is a
// no-op on healthy installs and on subsequent boots.
func (s *InboundService) MigrationRestoreVisionFlow() {
	db := database.GetDB()
	var inbounds []*model.Inbound
	if err := db.Model(&model.Inbound{}).
		Where("protocol = ?", model.VLESS).
		Find(&inbounds).Error; err != nil {
		logger.Warning("MigrationRestoreVisionFlow: load inbounds failed:", err)
		return
	}
	for _, ib := range inbounds {
		if ib.DisableFlow {
			continue
		}
		restored, changed := s.restoreVisionFlowForEligibleInbound(nil, ib.Settings, ib.StreamSettings, ib.Protocol)
		if !changed {
			continue
		}
		clients, err := s.GetClients(&model.Inbound{Settings: restored})
		if err != nil {
			logger.Warning("MigrationRestoreVisionFlow: parse clients for inbound", ib.Id, "failed:", err)
			continue
		}
		err = db.Transaction(func(tx *gorm.DB) error {
			if e := tx.Model(&model.Inbound{}).Where("id = ?", ib.Id).Update("settings", restored).Error; e != nil {
				return e
			}
			return s.clientService.SyncInbound(tx, ib.Id, clients)
		})
		if err != nil {
			logger.Warning("MigrationRestoreVisionFlow: update inbound", ib.Id, "failed:", err)
			continue
		}
		logger.Info("MigrationRestoreVisionFlow: restored XTLS Vision flow on inbound", ib.Id)
	}
}
