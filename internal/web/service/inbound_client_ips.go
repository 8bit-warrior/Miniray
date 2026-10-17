package service

import (
	"encoding/json"
	"errors"
	"sort"
	"time"

	"gorm.io/gorm"

	"github.com/8bit-warrior/Miniray/internal/database"
	"github.com/8bit-warrior/Miniray/internal/database/model"
)

func (s *InboundService) GetAllInboundClientIps() ([]model.InboundClientIps, error) {
	db := database.GetDB()
	var ips []model.InboundClientIps
	err := db.Model(&model.InboundClientIps{}).Find(&ips).Error
	return ips, err
}

const clientIpStaleAfterSeconds = int64(30 * 60)

// clientIpEntry is the on-disk shape of each element of InboundClientIps.Ips. Tags
// match job.IPWithTimestamp so the blob round-trips with the access.log scanner.
type clientIpEntry struct {
	IP        string `json:"ip"`
	Timestamp int64  `json:"timestamp"`
}

// mergeClientIpEntries unions old and incoming IP observations, dropping anything
// older than cutoff, keeping the most recent timestamp per IP, and returning the
// result sorted newest-first.
func mergeClientIpEntries(old, incoming []clientIpEntry, cutoff int64) []clientIpEntry {
	ipMap := make(map[string]int64, len(old)+len(incoming))
	for _, e := range old {
		if e.Timestamp < cutoff {
			continue
		}
		ipMap[e.IP] = e.Timestamp
	}
	for _, e := range incoming {
		if e.Timestamp < cutoff {
			continue
		}
		if cur, ok := ipMap[e.IP]; !ok || e.Timestamp > cur {
			ipMap[e.IP] = e.Timestamp
		}
	}
	out := make([]clientIpEntry, 0, len(ipMap))
	for ip, ts := range ipMap {
		out = append(out, clientIpEntry{IP: ip, Timestamp: ts})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Timestamp > out[j].Timestamp })
	return out
}

// ClientIpCasRetries bounds the re-reads after losing a compare-and-set. Running
// out is an error, so the caller retries on its next schedule instead of dropping IPs.
const ClientIpCasRetries = 8

// CasUpdateInboundClientIps writes newIps only while the row still holds expectedIps;
// updated=false with a nil error means another writer won and the caller must re-read.
func CasUpdateInboundClientIps(tx *gorm.DB, id int, expectedIps, newIps string) (updated bool, err error) {
	res := tx.Model(&model.InboundClientIps{}).
		Where("id = ? AND ips = ?", id, expectedIps).
		Update("ips", newIps)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected == 1, nil
}

func (s *InboundService) UpdateClientIPs(tx *gorm.DB, oldEmail string, newEmail string) error {
	// The caller only renames onto a free identity, so a row already sitting on
	// newEmail is stale tracking data — drop it instead of failing the edit.
	if oldEmail != newEmail {
		if err := tx.Where("client_email = ?", newEmail).Delete(model.InboundClientIps{}).Error; err != nil {
			return err
		}
	}
	return tx.Model(model.InboundClientIps{}).Where("client_email = ?", oldEmail).Update("client_email", newEmail).Error
}

func (s *InboundService) DelClientIPs(tx *gorm.DB, email string) error {
	return tx.Where("client_email = ?", email).Delete(model.InboundClientIps{}).Error
}

func (s *InboundService) delClientIPsByEmails(tx *gorm.DB, emails []string) error {
	const chunk = 400
	for start := 0; start < len(emails); start += chunk {
		end := min(start+chunk, len(emails))
		if err := tx.Where("client_email IN ?", emails[start:end]).Delete(model.InboundClientIps{}).Error; err != nil {
			return err
		}
	}
	return nil
}

func (s *InboundService) GetInboundClientIps(clientEmail string) (string, error) {
	db := database.GetDB()
	InboundClientIps := &model.InboundClientIps{}
	err := db.Model(model.InboundClientIps{}).Where("client_email = ?", clientEmail).First(InboundClientIps).Error
	if err != nil {
		return "", err
	}

	if InboundClientIps.Ips == "" {
		return "", nil
	}

	// Try to parse as new format (with timestamps)
	type IPWithTimestamp struct {
		IP        string `json:"ip"`
		Timestamp int64  `json:"timestamp"`
	}

	var ipsWithTime []IPWithTimestamp
	err = json.Unmarshal([]byte(InboundClientIps.Ips), &ipsWithTime)

	// If successfully parsed as new format, return with timestamps
	if err == nil && len(ipsWithTime) > 0 {
		return InboundClientIps.Ips, nil
	}

	// Otherwise, assume it's old format (simple string array)
	// Try to parse as simple array and convert to new format
	var oldIps []string
	err = json.Unmarshal([]byte(InboundClientIps.Ips), &oldIps)
	if err == nil && len(oldIps) > 0 {
		// Convert old format to new format with current timestamp
		newIpsWithTime := make([]IPWithTimestamp, len(oldIps))
		for i, ip := range oldIps {
			newIpsWithTime[i] = IPWithTimestamp{
				IP:        ip,
				Timestamp: time.Now().Unix(),
			}
		}
		result, _ := json.Marshal(newIpsWithTime)
		return string(result), nil
	}

	// Return as-is if parsing fails
	return InboundClientIps.Ips, nil
}

func (s *InboundService) ClearClientIps(clientEmail string) error {
	db := database.GetDB()

	result := db.Model(model.InboundClientIps{}).
		Where("client_email = ?", clientEmail).
		Update("ips", "")
	err := result.Error
	if err != nil {
		return err
	}
	return nil
}

// PruneStaleClientIps enforces clientIpStaleAfterSeconds for rows the online
// scan no longer rewrites: an offline client's addresses must still expire.
func (s *InboundService) PruneStaleClientIps() error {
	db := database.GetDB()
	cutoff := time.Now().Unix() - clientIpStaleAfterSeconds

	var rows []model.InboundClientIps
	if err := db.Find(&rows).Error; err != nil {
		return err
	}
	for _, row := range rows {
		var entries []clientIpEntry
		if row.Ips != "" {
			// Legacy blobs without timestamps stay untouched; the next scan rewrites them.
			if err := json.Unmarshal([]byte(row.Ips), &entries); err != nil {
				continue
			}
		}
		kept := mergeClientIpEntries(nil, entries, cutoff)
		if len(kept) == 0 {
			if err := db.Delete(&model.InboundClientIps{}, row.Id).Error; err != nil {
				return err
			}
			continue
		}
		if len(kept) == len(entries) {
			continue
		}
		b, _ := json.Marshal(kept)
		if err := db.Model(&model.InboundClientIps{}).Where("id = ?", row.Id).Update("ips", string(b)).Error; err != nil {
			return err
		}
	}
	return nil
}

type ClientIpInfo struct {
	IP   string `json:"ip"`
	Time string `json:"time"`
}

func (s *InboundService) GetClientIpLog(email string) ([]ClientIpInfo, error) {
	raw, err := s.GetInboundClientIps(email)
	if errors.Is(err, gorm.ErrRecordNotFound) || raw == "" {
		return []ClientIpInfo{}, nil
	}
	if err != nil {
		return nil, err
	}
	var entries []clientIpEntry
	if err := json.Unmarshal([]byte(raw), &entries); err != nil {
		return nil, err
	}
	result := make([]ClientIpInfo, 0, len(entries))
	for _, e := range entries {
		entry := ClientIpInfo{IP: e.IP}
		if e.Timestamp > 0 {
			entry.Time = time.Unix(e.Timestamp, 0).Local().Format("2006-01-02 15:04:05")
		}
		result = append(result, entry)
	}
	return result, nil
}
