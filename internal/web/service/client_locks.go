package service

import (
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/8bit-warrior/Miniray/internal/database/model"
	"github.com/8bit-warrior/Miniray/internal/logger"

	"gorm.io/gorm"
)

var (
	recentlyDeletedMu sync.Mutex
	recentlyDeleted   = map[string]time.Time{}
)

const deleteTombstoneTTL = 90 * time.Second

var (
	inboundMutationLocksMu sync.Mutex
	inboundMutationLocks   = map[int]*sync.Mutex{}
)

func lockInbound(inboundId int) *sync.Mutex {
	inboundMutationLocksMu.Lock()
	m, ok := inboundMutationLocks[inboundId]
	if !ok {
		m = &sync.Mutex{}
		inboundMutationLocks[inboundId] = m
	}
	inboundMutationLocksMu.Unlock()
	m.Lock()
	return m
}

func compactOrphans(db *gorm.DB, clients []any) []any {
	if len(clients) == 0 {
		return clients
	}
	emails := make([]string, 0, len(clients))
	for _, c := range clients {
		cm, ok := c.(map[string]any)
		if !ok {
			continue
		}
		if e, _ := cm["email"].(string); e != "" {
			emails = append(emails, e)
		}
	}
	if len(emails) == 0 {
		return clients
	}
	existing := make(map[string]struct{}, len(emails))
	const orphanChunk = 400
	for start := 0; start < len(emails); start += orphanChunk {
		end := min(start+orphanChunk, len(emails))
		var found []string
		if err := db.Model(&model.ClientRecord{}).Where("email IN ?", emails[start:end]).Pluck("email", &found).Error; err != nil {
			logger.Warning("compactOrphans pluck:", err)
			return clients
		}
		for _, e := range found {
			existing[e] = struct{}{}
		}
	}
	if len(existing) == len(emails) {
		return clients
	}
	out := make([]any, 0, len(existing))
	for _, c := range clients {
		cm, ok := c.(map[string]any)
		if !ok {
			out = append(out, c)
			continue
		}
		e, _ := cm["email"].(string)
		if e == "" {
			out = append(out, c)
			continue
		}
		if _, ok := existing[e]; ok {
			out = append(out, c)
		}
	}
	return out
}

func tombstoneClientEmail(email string) {
	if email == "" {
		return
	}
	recentlyDeletedMu.Lock()
	defer recentlyDeletedMu.Unlock()
	recentlyDeleted[email] = time.Now()
	cutoff := time.Now().Add(-deleteTombstoneTTL)
	for e, ts := range recentlyDeleted {
		if ts.Before(cutoff) {
			delete(recentlyDeleted, e)
		}
	}
}

func withdrawClientTombstones(emails ...string) {
	if len(emails) == 0 {
		return
	}
	recentlyDeletedMu.Lock()
	defer recentlyDeletedMu.Unlock()
	for _, email := range emails {
		if email != "" {
			delete(recentlyDeleted, email)
		}
	}
}

func tombstoneClientEmails(emails []string) {
	if len(emails) == 0 {
		return
	}
	now := time.Now()
	cutoff := now.Add(-deleteTombstoneTTL)
	recentlyDeletedMu.Lock()
	defer recentlyDeletedMu.Unlock()
	for _, email := range emails {
		if email != "" {
			recentlyDeleted[email] = now
		}
	}
	for e, ts := range recentlyDeleted {
		if ts.Before(cutoff) {
			delete(recentlyDeleted, e)
		}
	}
}

func isClientEmailTombstoned(email string) bool {
	if email == "" {
		return false
	}
	recentlyDeletedMu.Lock()
	defer recentlyDeletedMu.Unlock()
	ts, ok := recentlyDeleted[email]
	if !ok {
		return false
	}
	if time.Since(ts) > deleteTombstoneTTL {
		delete(recentlyDeleted, email)
		return false
	}
	return true
}

func dedupeSettingsClients(settings string) (string, bool) {
	if settings == "" {
		return settings, false
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(settings), &parsed); err != nil {
		return settings, false
	}
	clients, _ := parsed["clients"].([]any)
	if len(clients) < 2 {
		return settings, false
	}
	seen := make(map[string]struct{}, len(clients))
	kept := make([]any, 0, len(clients))
	for _, c := range clients {
		if cm, ok := c.(map[string]any); ok {
			if email, _ := cm["email"].(string); email != "" {
				key := strings.ToLower(email)
				if _, dup := seen[key]; dup {
					continue
				}
				seen[key] = struct{}{}
			}
		}
		kept = append(kept, c)
	}
	if len(kept) == len(clients) {
		return settings, false
	}
	parsed["clients"] = kept
	b, err := json.MarshalIndent(parsed, "", "  ")
	if err != nil {
		return settings, false
	}
	return string(b), true
}
