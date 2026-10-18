package service

import (
	"testing"

	"github.com/8bit-warrior/Miniray/internal/database"
	"github.com/8bit-warrior/Miniray/internal/database/model"
	"github.com/8bit-warrior/Miniray/internal/web/runtime"
	"github.com/8bit-warrior/Miniray/internal/xray"
)

func TestDelDepletedClientsCleansRuntimeAfterCommit(t *testing.T) {
	setupConflictDB(t)
	mgr := runtime.NewManager(runtime.LocalDeps{APIPort: func() int { return 0 }})
	fake := &fakeRuntime{}
	mgr.SetLocalRuntimeOverride(fake)
	runtime.SetManager(mgr)
	t.Cleanup(func() { runtime.SetManager(nil) })

	seedInboundConflict(t, "depleted-only", "", 46303, model.VLESS, `{"network":"tcp"}`,
		`{"clients":[{"email":"gone","enable":true}]}`)
	ib := loadInboundByTag(t, "depleted-only")
	seedClientTraffic(t, ib.Id, "gone", true)
	if err := database.GetDB().Model(&xray.ClientTraffic{}).Where("email = ?", "gone").
		Updates(map[string]any{"up": 100, "total": 100, "reset": 0}).Error; err != nil {
		t.Fatalf("deplete traffic: %v", err)
	}

	if err := (&InboundService{}).DelDepletedClients(-1); err != nil {
		t.Fatalf("DelDepletedClients: %v", err)
	}
	if got := fake.delInbound.Load(); got != 1 {
		t.Fatalf("runtime DelInbound calls = %d, want 1 after commit", got)
	}
}
