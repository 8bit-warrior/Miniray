package service

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/8bit-warrior/Miniray/internal/database"
	"github.com/8bit-warrior/Miniray/internal/database/dbtest"
	"github.com/8bit-warrior/Miniray/internal/database/model"
)

// setupClientIpTestDB spins up a throwaway SQLite database (migrations + seeders)
// for a single test, mirroring the harness used by the other service tests.
func setupClientIpTestDB(t *testing.T) {
	t.Helper()
	dbDir := t.TempDir()
	t.Setenv("MINIRAY_DB_FOLDER", dbDir)
	dbtest.InitDB(t, filepath.Join(dbDir, "miniray.db"))
}

func marshalIps(t *testing.T, entries ...clientIpEntry) string {
	t.Helper()
	b, err := json.Marshal(entries)
	if err != nil {
		t.Fatalf("marshal ips: %v", err)
	}
	return string(b)
}

// readClientIps returns the stored IP entries for an email as a map[ip]timestamp,
// plus whether the row exists at all.
func readClientIps(t *testing.T, email string) (map[string]int64, bool) {
	t.Helper()
	var row model.InboundClientIps
	err := database.GetDB().Where("client_email = ?", email).First(&row).Error
	if database.IsNotFound(err) {
		return nil, false
	}
	if err != nil {
		t.Fatalf("read client ips for %s: %v", email, err)
	}
	var entries []clientIpEntry
	if row.Ips != "" {
		if err := json.Unmarshal([]byte(row.Ips), &entries); err != nil {
			t.Fatalf("unmarshal stored ips for %s: %v", email, err)
		}
	}
	out := make(map[string]int64, len(entries))
	for _, e := range entries {
		out[e.IP] = e.Timestamp
	}
	return out, true
}

func TestCasUpdateInboundClientIps_MatchAndMismatch(t *testing.T) {
	setupClientIpTestDB(t)
	db := database.GetDB()
	now := time.Now().Unix()

	seed := &model.InboundClientIps{
		ClientEmail: "cas@x",
		Ips:         marshalIps(t, clientIpEntry{IP: "1.1.1.1", Timestamp: now}),
	}
	if err := db.Create(seed).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}

	next := marshalIps(t, clientIpEntry{IP: "2.2.2.2", Timestamp: now})
	ok, err := CasUpdateInboundClientIps(db, seed.Id, "not-the-blob", next)
	if err != nil {
		t.Fatalf("stale CAS: %v", err)
	}
	if ok {
		t.Fatalf("CAS with wrong expected must not update")
	}
	ips, _ := readClientIps(t, "cas@x")
	if ips["1.1.1.1"] != now || len(ips) != 1 {
		t.Fatalf("row changed on stale CAS: %v", ips)
	}

	ok, err = CasUpdateInboundClientIps(db, seed.Id, seed.Ips, next)
	if err != nil {
		t.Fatalf("fresh CAS: %v", err)
	}
	if !ok {
		t.Fatalf("CAS with matching expected must update")
	}
	ips, _ = readClientIps(t, "cas@x")
	if ips["2.2.2.2"] != now || len(ips) != 1 {
		t.Fatalf("fresh CAS did not land: %v", ips)
	}
}
