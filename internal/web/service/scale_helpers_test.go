package service

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/8bit-warrior/Miniray/internal/database"
	"github.com/8bit-warrior/Miniray/internal/database/dbtest"
	"github.com/8bit-warrior/Miniray/internal/database/model"
	miniraylogger "github.com/8bit-warrior/Miniray/internal/logger"
	"github.com/8bit-warrior/Miniray/internal/xray"

	"github.com/google/uuid"
	"github.com/op/go-logging"
	"gorm.io/gorm"
)

func makeScaleClients(n int) []model.Client {
	clients := make([]model.Client, n)
	for i := range clients {
		clients[i] = model.Client{
			ID:     uuid.NewString(),
			Email:  fmt.Sprintf("user-%07d@scale", i),
			SubID:  fmt.Sprintf("sub-%07d", i),
			Enable: true,
		}
	}
	return clients
}

// setupScaleDB opens a SQLite database when scale tests are enabled.
func setupScaleDB(t *testing.T) {
	t.Helper()
	miniraylogger.InitLogger(logging.ERROR)

	switch strings.ToLower(strings.TrimSpace(os.Getenv("MINIRAY_SCALE_TEST"))) {
	case "1", "true", "yes":
		dbPath := strings.TrimSpace(os.Getenv("MINIRAY_SCALE_DB_PATH"))
		if dbPath == "" {
			dbPath = filepath.Join(t.TempDir(), "scale.db")
		}
		dbtest.InitDB(t, dbPath)
		return
	}

	t.Skip("set MINIRAY_SCALE_TEST=1 to run the SQLite scale benchmark")
}

// scaleSizes returns the default size ladder unless MINIRAY_SCALE_SIZES overrides
// it with a comma-separated list (e.g. "500000" or "10000,100000,500000").
func scaleSizes(t *testing.T, def ...int) []int {
	t.Helper()
	raw := strings.TrimSpace(os.Getenv("MINIRAY_SCALE_SIZES"))
	if raw == "" {
		return def
	}
	var out []int
	for part := range strings.SplitSeq(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		n, err := strconv.Atoi(part)
		if err != nil || n <= 0 {
			t.Fatalf("MINIRAY_SCALE_SIZES: invalid size %q", part)
		}
		out = append(out, n)
	}
	if len(out) == 0 {
		return def
	}
	return out
}

type scaleDataset struct {
	inboundIds []int
	tags       []string
	emails     []string
	perInbound [][]model.Client
}

// seedScaleDataset seeds n healthy clients (future expiry, unfilled quota)
// spread across numInbounds inbounds, writing inbounds, clients,
// client_inbounds and client_traffics directly in one transaction — orders of
// magnitude faster than SyncInbound and one fsync instead of thousands.
func seedScaleDataset(t *testing.T, n, numInbounds int) scaleDataset {
	t.Helper()
	db := database.GetDB()
	resetScaleTables(t, db, "inbounds", "clients", "client_inbounds", "client_traffics")

	clients := makeScaleClients(n)
	exp := time.Now().AddDate(1, 0, 0).UnixMilli()
	for i := range clients {
		clients[i].ExpiryTime = exp
		clients[i].TotalGB = 100 << 30
	}

	ds := scaleDataset{emails: emailsOf(clients)}
	start := time.Now()
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatalf("begin seed tx: %v", tx.Error)
	}
	committed := false
	defer func() {
		if !committed {
			tx.Rollback()
		}
	}()

	per := n / numInbounds
	for i := range numInbounds {
		lo, hi := i*per, (i+1)*per
		if i == numInbounds-1 {
			hi = n
		}
		chunk := clients[lo:hi]
		ib := &model.Inbound{
			UserId:   1,
			Tag:      fmt.Sprintf("scale-%d-%d", n, i),
			Enable:   true,
			Port:     41000 + i,
			Protocol: model.VLESS,
			Settings: clientsSettings(t, chunk),
		}
		if err := tx.Create(ib).Error; err != nil {
			t.Fatalf("seed inbound %d: %v", i, err)
		}

		records := make([]*model.ClientRecord, len(chunk))
		for j := range chunk {
			records[j] = chunk[j].ToRecord()
		}
		if err := tx.CreateInBatches(records, 500).Error; err != nil {
			t.Fatalf("seed clients %d: %v", i, err)
		}

		links := make([]model.ClientInbound, len(records))
		for j := range records {
			links[j] = model.ClientInbound{ClientId: records[j].Id, InboundId: ib.Id}
		}
		if err := tx.CreateInBatches(links, 1000).Error; err != nil {
			t.Fatalf("seed client_inbounds %d: %v", i, err)
		}

		traffics := make([]xray.ClientTraffic, len(chunk))
		for j := range chunk {
			traffics[j] = xray.ClientTraffic{
				InboundId:  ib.Id,
				Email:      chunk[j].Email,
				Enable:     true,
				Total:      chunk[j].TotalGB,
				ExpiryTime: chunk[j].ExpiryTime,
			}
		}
		if err := tx.CreateInBatches(traffics, 1000).Error; err != nil {
			t.Fatalf("seed client_traffics %d: %v", i, err)
		}

		ds.inboundIds = append(ds.inboundIds, ib.Id)
		ds.tags = append(ds.tags, ib.Tag)
		ds.perInbound = append(ds.perInbound, chunk)
	}

	if err := tx.Commit().Error; err != nil {
		t.Fatalf("commit seed tx: %v", err)
	}
	committed = true
	db.Exec("ANALYZE")
	t.Logf("seeded N=%d across %d inbound(s) in %v", n, numInbounds, time.Since(start).Round(time.Millisecond))
	return ds
}

// sampleEmails picks k evenly spaced emails so active clients span the id range.
func sampleEmails(emails []string, k int) []string {
	if k >= len(emails) {
		return emails
	}
	out := make([]string, 0, k)
	step := len(emails) / k
	for i := 0; i < len(emails) && len(out) < k; i += step {
		out = append(out, emails[i])
	}
	return out
}

// resetScaleTables clears SQLite tables and their autoincrement counters.
func resetScaleTables(t *testing.T, db *gorm.DB, tables ...string) {
	t.Helper()
	for _, tbl := range tables {
		if err := db.Exec("DELETE FROM " + tbl).Error; err != nil {
			t.Fatalf("delete %s: %v", tbl, err)
		}
	}
	// Best-effort id reset; sqlite_sequence is absent until the first insert.
	db.Exec("DELETE FROM sqlite_sequence")
}
