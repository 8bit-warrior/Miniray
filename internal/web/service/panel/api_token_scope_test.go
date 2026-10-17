package panel

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/8bit-warrior/Miniray/internal/database"
	"github.com/8bit-warrior/Miniray/internal/database/dbtest"
	"github.com/8bit-warrior/Miniray/internal/database/model"
	"github.com/8bit-warrior/Miniray/internal/util/crypto"
)

func setupAPITokenTestDB(t *testing.T) {
	t.Helper()
	dbDir := t.TempDir()
	t.Setenv("MINIRAY_DB_FOLDER", dbDir)
	dbtest.InitDB(t, filepath.Join(dbDir, "miniray.db"))
}

func TestAPITokenDeleteAndEnableRequireExpectedScope(t *testing.T) {
	setupAPITokenTestDB(t)
	svc := &ApiTokenService{}
	created, err := svc.Create("scoped", model.ApiScopeMonitor, 0)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := svc.SetEnabledExpectedScope(created.Id, model.ApiScopeAdmin, false); err == nil {
		t.Fatal("wrong expected scope changed token state")
	}
	if _, ok := svc.MatchToken(created.Token); !ok {
		t.Fatal("wrong-scope update disabled token")
	}
	if err := svc.DeleteExpectedScope(created.Id, model.ApiScopeAdmin); err == nil {
		t.Fatal("wrong expected scope deleted token")
	}
	if err := svc.DeleteExpectedScope(created.Id, model.ApiScopeMonitor); err != nil {
		t.Fatalf("DeleteExpectedScope: %v", err)
	}
}

func TestAPITokenEmptyExpectedScopeCannotTargetAdmin(t *testing.T) {
	setupAPITokenTestDB(t)
	svc := &ApiTokenService{}
	created, err := svc.Create("admin-token", model.ApiScopeAdmin, 0)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := svc.DisableExpectedScope(created.Id, ""); err == nil {
		t.Fatal("empty expected scope defaulted to admin")
	}
	if _, ok := svc.MatchToken(created.Token); !ok {
		t.Fatal("empty expected scope disabled the admin token")
	}
}

func TestAPITokenAdditiveDefaultsPreserveLegacyAccess(t *testing.T) {
	setupAPITokenTestDB(t)
	const plaintext = "legacy-token"
	if err := database.GetDB().Exec(
		"INSERT INTO api_tokens (name, token, enabled, created_at) VALUES (?, ?, ?, ?)",
		"legacy", crypto.HashTokenSHA256(plaintext), true, time.Now().Unix(),
	).Error; err != nil {
		t.Fatalf("insert legacy-shaped token: %v", err)
	}
	row, ok := (&ApiTokenService{}).MatchToken(plaintext)
	if !ok {
		t.Fatal("legacy-shaped token no longer authenticates")
	}
	if row.Scope != model.ApiScopeAdmin || row.ExpiresAt != 0 {
		t.Fatalf("legacy defaults = scope %q expiry %d, want admin/0", row.Scope, row.ExpiresAt)
	}
}
