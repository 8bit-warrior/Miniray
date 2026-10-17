package service

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"

	"gorm.io/gorm"

	"github.com/8bit-warrior/Miniray/internal/database"
	"github.com/8bit-warrior/Miniray/internal/web/runtime"
	"github.com/8bit-warrior/Miniray/internal/xray"

	"github.com/8bit-warrior/Miniray/internal/database/dbtest"
	"github.com/8bit-warrior/Miniray/internal/database/model"
)

type fakeRuntime struct {
	addInbound    atomic.Int32
	delInbound    atomic.Int32
	addClient     atomic.Int32
	deleteClient  atomic.Int32
	deleteUser    atomic.Int32
	updateInbound atomic.Int32
	updateSubSort atomic.Int32
	updateUser    atomic.Int32
}

func (f *fakeRuntime) Name() string { return "fake-node" }

func (f *fakeRuntime) AddInbound(context.Context, *model.Inbound) error {
	f.addInbound.Add(1)
	return nil
}

func (f *fakeRuntime) DelInbound(context.Context, *model.Inbound) error {
	f.delInbound.Add(1)
	return nil
}

func (f *fakeRuntime) UpdateInbound(context.Context, *model.Inbound, *model.Inbound) error {
	f.updateInbound.Add(1)
	return nil
}

func (f *fakeRuntime) SetInboundSubSortIndex(context.Context, *model.Inbound, int) error {
	f.updateSubSort.Add(1)
	return nil
}

func (f *fakeRuntime) AddUser(context.Context, *model.Inbound, map[string]any) error { return nil }

func (f *fakeRuntime) RemoveUser(context.Context, *model.Inbound, string) error { return nil }

func (f *fakeRuntime) UpdateUser(context.Context, *model.Inbound, string, model.Client) error {
	f.updateUser.Add(1)
	return nil
}

func (f *fakeRuntime) DeleteUser(context.Context, *model.Inbound, string) error {
	f.deleteUser.Add(1)
	return nil
}

func (f *fakeRuntime) DeleteClient(context.Context, string) error {
	f.deleteClient.Add(1)
	return nil
}

func (f *fakeRuntime) AddClient(context.Context, *model.Inbound, model.Client) error {
	f.addClient.Add(1)
	return nil
}
func (f *fakeRuntime) RestartXray(context.Context) error { return nil }
func (f *fakeRuntime) ResetClientTraffic(context.Context, *model.Inbound, string) error {
	return nil
}
func (f *fakeRuntime) ResetInboundTraffic(context.Context, *model.Inbound) error { return nil }
func (f *fakeRuntime) ResetAllTraffics(context.Context) error                    { return nil }

func useTestRuntimeManager(t *testing.T) *runtime.Manager {
	t.Helper()
	prev := runtime.GetManager()
	mgr := runtime.NewManager(runtime.LocalDeps{APIPort: func() int { return 0 }, SetNeedRestart: func() {}})
	runtime.SetManager(mgr)
	t.Cleanup(func() { runtime.SetManager(prev) })
	return mgr
}

func readTraffic(t *testing.T, db *gorm.DB, email string) xray.ClientTraffic {
	t.Helper()
	var ct xray.ClientTraffic
	if err := db.Model(xray.ClientTraffic{}).Where("email = ?", email).First(&ct).Error; err != nil {
		t.Fatalf("read client_traffics %q: %v", email, err)
	}
	return ct
}

func initTrafficTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dbDir := t.TempDir()
	t.Setenv("MINIRAY_DB_FOLDER", dbDir)
	dbtest.InitDB(t, filepath.Join(dbDir, "miniray.db"))
	return database.GetDB()
}
