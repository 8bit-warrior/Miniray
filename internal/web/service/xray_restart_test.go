package service

import (
	"path/filepath"
	"testing"

	"github.com/8bit-warrior/Miniray/internal/xray"
)

func TestRestartXrayRespectsManualStop(t *testing.T) {
	setupSettingTestDB(t)
	if err := (&SettingService{}).saveSetting("xrayTemplateConfig", "{ not valid json"); err != nil {
		t.Fatalf("seed template: %v", err)
	}
	t.Cleanup(func() { isManuallyStopped.Store(false) })

	isManuallyStopped.Store(true)
	_ = (&XrayService{}).RestartXray(false)

	if !isManuallyStopped.Load() {
		t.Fatal("a non-forced restart cleared a deliberate manual stop and would revive xray")
	}
}

func TestApplyPendingRestartReArmsFlagOnFailure(t *testing.T) {
	setupSettingTestDB(t)
	if err := (&SettingService{}).saveSetting("xrayTemplateConfig", "{ not valid json"); err != nil {
		t.Fatalf("seed template: %v", err)
	}
	t.Cleanup(func() {
		isManuallyStopped.Store(false)
		isNeedXrayRestart.Store(false)
	})
	isManuallyStopped.Store(false)

	svc := &XrayService{}
	svc.SetToNeedRestart()
	svc.ApplyPendingRestart()

	if !isNeedXrayRestart.Load() {
		t.Fatal("a failed restart must re-arm the need-restart flag so the pending config change is retried")
	}
}

func TestSharedManualCoreStopSuppressesWatchdogAndReconcile(t *testing.T) {
	setupSettingTestDB(t)
	t.Setenv("MINIRAY_SEPARATE_CORE", "1")
	t.Setenv("MINIRAY_CORE_STOP_FILE", filepath.Join(t.TempDir(), "manual-stop"))
	if err := (&SettingService{}).saveSetting("xrayTemplateConfig", "{ not valid json"); err != nil {
		t.Fatal(err)
	}
	if err := xray.SetIndependentCoreManuallyStopped(true); err != nil {
		t.Fatal(err)
	}
	isManuallyStopped.Store(false)
	svc := &XrayService{}
	if svc.DidXrayCrash() {
		t.Fatal("a deliberately stopped core must not be treated as crashed")
	}
	if err := svc.RestartXray(false); err != nil {
		t.Fatalf("automatic reconcile ignored shared manual stop: %v", err)
	}
}
