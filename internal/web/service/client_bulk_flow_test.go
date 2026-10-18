package service

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/8bit-warrior/Miniray/internal/database"
	"github.com/8bit-warrior/Miniray/internal/database/model"
	"github.com/8bit-warrior/Miniray/internal/xray"
)

// mkInboundStream is mkInbound with explicit stream settings, needed to make an
// inbound flow-eligible (VLESS + tcp + reality/tls).
func mkInboundStream(t *testing.T, port int, proto model.Protocol, settings, stream string) *model.Inbound {
	t.Helper()
	ib := &model.Inbound{
		Tag:            string(proto) + "-stream-" + emailSafe(port),
		Enable:         true,
		Port:           port,
		Protocol:       proto,
		Settings:       settings,
		StreamSettings: stream,
	}
	if err := database.GetDB().Create(ib).Error; err != nil {
		t.Fatalf("create inbound %d: %v", port, err)
	}
	return ib
}

func emailSafe(port int) string {
	return string(rune('a'+port%26)) + string(rune('a'+(port/26)%26))
}

func flowOf(t *testing.T, svc *ClientService, email string) string {
	t.Helper()
	rec, err := svc.GetRecordByEmail(nil, email)
	if err != nil {
		t.Fatalf("GetRecordByEmail(%q): %v", email, err)
	}
	return rec.Flow
}

const (
	realityStream = `{"network":"tcp","security":"reality"}`
	wsStream      = `{"network":"ws","security":"none"}`
)

// TestBulkAdjust_FlowSetAndClear covers the happy path: a vision flow is applied
// on an eligible VLESS inbound and later cleared with the "none" directive. Both
// transitions are real config changes, so they must request a restart.
func TestBulkAdjust_FlowSetAndClear(t *testing.T) {
	setupBulkDB(t)
	svc := &ClientService{}
	inboundSvc := &InboundService{}

	clients := []model.Client{
		{Email: "f1@x", ID: "11111111-1111-1111-1111-111111111111", SubID: "f1", Enable: true},
		{Email: "f2@x", ID: "22222222-2222-2222-2222-222222222222", SubID: "f2", Enable: true},
	}
	ib := mkInboundStream(t, 30001, model.VLESS, clientsSettings(t, clients), realityStream)
	if err := svc.SyncInbound(nil, ib.Id, clients); err != nil {
		t.Fatalf("seed: %v", err)
	}
	emails := emailsOf(clients)

	// Set vision flow.
	res, restart, err := svc.BulkAdjust(inboundSvc, emails, 0, 0, "xtls-rprx-vision-udp443", nil)
	if err != nil {
		t.Fatalf("BulkAdjust set: %v", err)
	}
	if res.Adjusted != 2 {
		t.Fatalf("expected 2 adjusted, got %d (skipped=%v)", res.Adjusted, res.Skipped)
	}
	if !restart {
		t.Fatalf("setting flow should request a restart")
	}
	for _, e := range emails {
		if got := flowOf(t, svc, e); got != "xtls-rprx-vision-udp443" {
			t.Fatalf("%s flow = %q, want xtls-rprx-vision-udp443", e, got)
		}
	}

	// Setting the same flow again is a no-op: honored (counted) but no restart.
	if _, restart2, err := svc.BulkAdjust(inboundSvc, emails, 0, 0, "xtls-rprx-vision-udp443", nil); err != nil {
		t.Fatalf("BulkAdjust idempotent: %v", err)
	} else if restart2 {
		t.Fatalf("re-setting identical flow should not request a restart")
	}

	// Clear flow.
	cres, crestart, err := svc.BulkAdjust(inboundSvc, emails, 0, 0, "none", nil)
	if err != nil {
		t.Fatalf("BulkAdjust clear: %v", err)
	}
	if cres.Adjusted != 2 {
		t.Fatalf("expected 2 cleared, got %d (skipped=%v)", cres.Adjusted, cres.Skipped)
	}
	if !crestart {
		t.Fatalf("clearing flow should request a restart")
	}
	for _, e := range emails {
		if got := flowOf(t, svc, e); got != "" {
			t.Fatalf("%s flow = %q, want empty after clear", e, got)
		}
	}
}

// TestBulkAdjust_FlowIneligibleSkipped verifies a vision flow is refused on an
// inbound that cannot carry it (ws transport), reported as skipped, and the
// client's flow is left untouched.
func TestBulkAdjust_FlowIneligibleSkipped(t *testing.T) {
	setupBulkDB(t)
	svc := &ClientService{}
	inboundSvc := &InboundService{}

	clients := []model.Client{
		{Email: "ws1@x", ID: "33333333-3333-3333-3333-333333333333", SubID: "ws1", Enable: true},
	}
	ib := mkInboundStream(t, 30101, model.VLESS, clientsSettings(t, clients), wsStream)
	if err := svc.SyncInbound(nil, ib.Id, clients); err != nil {
		t.Fatalf("seed: %v", err)
	}

	res, restart, err := svc.BulkAdjust(inboundSvc, []string{"ws1@x"}, 0, 0, "xtls-rprx-vision", nil)
	if err != nil {
		t.Fatalf("BulkAdjust: %v", err)
	}
	if res.Adjusted != 0 {
		t.Fatalf("ineligible inbound should adjust nothing, got %d", res.Adjusted)
	}
	if restart {
		t.Fatalf("no change should not request a restart")
	}
	if len(res.Skipped) != 1 || res.Skipped[0].Email != "ws1@x" {
		t.Fatalf("expected ws1@x in skipped, got %v", res.Skipped)
	}
	if got := flowOf(t, svc, "ws1@x"); got != "" {
		t.Fatalf("flow should stay empty on ineligible inbound, got %q", got)
	}
}

// TestBulkAdjust_NoDirectiveErrors guards the relaxed precondition: with no
// days, traffic, or flow set there is nothing to do.
func TestBulkAdjust_NoDirectiveErrors(t *testing.T) {
	setupBulkDB(t)
	svc := &ClientService{}
	inboundSvc := &InboundService{}

	if _, _, err := svc.BulkAdjust(inboundSvc, []string{"any@x"}, 0, 0, "", nil); err == nil {
		t.Fatalf("expected error when no adjustment is specified")
	}
	// An unknown flow directive is ignored (treated as ""), so it also errors.
	if _, _, err := svc.BulkAdjust(inboundSvc, []string{"any@x"}, 0, 0, "bogus-flow", nil); err == nil {
		t.Fatalf("unknown flow should be ignored and error like an empty directive")
	}
}

// TestBulkAdjust_DaysApplyDespiteIneligibleFlow is the regression for the review
// blocker: when a client on a flow-ineligible inbound is adjusted with BOTH a
// days/traffic delta AND a flow directive, the days/traffic change must still be
// persisted to ClientTraffic (not just the inbound JSON / ClientRecord) and the
// client must count as adjusted, while the unhonored flow is reported separately.
func TestBulkAdjust_DaysApplyDespiteIneligibleFlow(t *testing.T) {
	setupBulkDB(t)
	svc := &ClientService{}
	inboundSvc := &InboundService{}

	const day = int64(24 * 60 * 60 * 1000)
	const gb = int64(1) << 30
	baseExpiry := time.Now().UnixMilli() + 30*day
	baseTotal := 10 * gb

	clients := []model.Client{
		{Email: "mix@x", ID: "44444444-4444-4444-4444-444444444444", SubID: "mix", Enable: true, ExpiryTime: baseExpiry, TotalGB: baseTotal},
	}
	ib := mkInboundStream(t, 30201, model.VLESS, clientsSettings(t, clients), wsStream)
	if err := svc.SyncInbound(nil, ib.Id, clients); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// ClientTraffic is the store the enforcement job reads; seed it to match.
	if err := database.GetDB().Create(&xray.ClientTraffic{Email: "mix@x", Enable: true, ExpiryTime: baseExpiry, Total: baseTotal}).Error; err != nil {
		t.Fatalf("seed traffic: %v", err)
	}

	res, _, err := svc.BulkAdjust(inboundSvc, []string{"mix@x"}, 7, gb, "xtls-rprx-vision", nil)
	if err != nil {
		t.Fatalf("BulkAdjust: %v", err)
	}
	if res.Adjusted != 1 {
		t.Fatalf("days/traffic should still be applied: Adjusted=%d skipped=%v", res.Adjusted, res.Skipped)
	}
	if len(res.Skipped) != 1 || res.Skipped[0].Email != "mix@x" {
		t.Fatalf("expected mix@x reported for the unhonored flow, got %v", res.Skipped)
	}

	wantExpiry := baseExpiry + 7*day
	wantTotal := baseTotal + gb

	// ClientRecord (inbound-derived) advanced.
	if rec, err := svc.GetRecordByEmail(nil, "mix@x"); err != nil {
		t.Fatalf("record: %v", err)
	} else if rec.ExpiryTime != wantExpiry || rec.TotalGB != wantTotal {
		t.Fatalf("ClientRecord not advanced: expiry=%d total=%d", rec.ExpiryTime, rec.TotalGB)
	}

	// ClientTraffic advanced in lockstep — no divergence.
	var ct xray.ClientTraffic
	if err := database.GetDB().Where("email = ?", "mix@x").First(&ct).Error; err != nil {
		t.Fatalf("traffic row: %v", err)
	}
	if ct.ExpiryTime != wantExpiry || ct.Total != wantTotal {
		t.Fatalf("ClientTraffic diverged: expiry=%d total=%d, want expiry=%d total=%d", ct.ExpiryTime, ct.Total, wantExpiry, wantTotal)
	}

	// Flow left untouched on the ineligible inbound.
	if got := flowOf(t, svc, "mix@x"); got != "" {
		t.Fatalf("flow should stay empty on ineligible inbound, got %q", got)
	}
}

// TestBulkAdjust_HwidLimit verifies setting and clearing HWID limit in bulk.
func TestBulkAdjust_HwidLimit(t *testing.T) {
	setupBulkDB(t)
	svc := &ClientService{}
	inboundSvc := &InboundService{}

	clients := []model.Client{
		{Email: "h1@x", ID: "11111111-1111-1111-1111-111111111111", SubID: "sub-h1", Enable: true},
		{Email: "h2@x", ID: "22222222-2222-2222-2222-222222222222", SubID: "sub-h2", Enable: true},
	}
	ib := mkInbound(t, 30301, model.VLESS, clientsSettings(t, clients))
	if err := svc.SyncInbound(nil, ib.Id, clients); err != nil {
		t.Fatalf("seed: %v", err)
	}
	emails := emailsOf(clients)

	limit2 := 2
	res, restart, err := svc.BulkAdjust(inboundSvc, emails, 0, 0, "", &limit2)
	if err != nil {
		t.Fatalf("BulkAdjust hwid: %v", err)
	}
	if res.Adjusted != 2 {
		t.Fatalf("expected 2 adjusted, got %d", res.Adjusted)
	}
	if restart {
		t.Fatalf("hwid adjustment should not request xray restart")
	}
	for _, e := range emails {
		rec, rErr := svc.GetRecordByEmail(nil, e)
		if rErr != nil || rec.LimitHwid != 2 {
			t.Fatalf("%s limitHwid = %d (err=%v), want 2", e, rec.LimitHwid, rErr)
		}
	}

	// Reset to 0 (unlimited)
	limit0 := 0
	res0, _, err0 := svc.BulkAdjust(inboundSvc, emails, 0, 0, "", &limit0)
	if err0 != nil || res0.Adjusted != 2 {
		t.Fatalf("BulkAdjust hwid 0: err=%v, res=%+v", err0, res0)
	}
	for _, e := range emails {
		rec, _ := svc.GetRecordByEmail(nil, e)
		if rec.LimitHwid != 0 {
			t.Fatalf("%s limitHwid = %d, want 0", e, rec.LimitHwid)
		}
	}
}

// TestBulkAdjust_UnchangedClientKeepsUpdatedAt pins the updated_at stamp to the
// client that actually changed: an untouched client must not be re-stamped only
// because a client earlier in the same inbound's array was adjusted.
func TestBulkAdjust_UnchangedClientKeepsUpdatedAt(t *testing.T) {
	setupBulkDB(t)
	svc := &ClientService{}
	inboundSvc := &InboundService{}

	const day = int64(24 * 60 * 60 * 1000)
	const seeded = int64(1600000000000)
	baseExpiry := time.Now().UnixMilli() + 30*day

	// chg@x is listed first and takes the expiry bump; keep@x has unlimited
	// expiry on a ws inbound, so the same call changes nothing for it.
	clients := []model.Client{
		{Email: "chg@x", ID: "88888888-8888-8888-8888-888888888888", SubID: "chg", Enable: true, ExpiryTime: baseExpiry, UpdatedAt: seeded},
		{Email: "keep@x", ID: "99999999-9999-9999-9999-999999999999", SubID: "keep", Enable: true, UpdatedAt: seeded},
	}
	ib := mkInboundStream(t, 30801, model.VLESS, clientsSettings(t, clients), wsStream)
	if err := svc.SyncInbound(nil, ib.Id, clients); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := database.GetDB().Create(&xray.ClientTraffic{Email: "chg@x", Enable: true, ExpiryTime: baseExpiry}).Error; err != nil {
		t.Fatalf("seed traffic: %v", err)
	}

	// The flow directive is what keeps keep@x in the plan; the ws inbound cannot
	// carry it, so the directive is not itself a change for either client.
	if _, _, err := svc.BulkAdjust(inboundSvc, emailsOf(clients), 7, 0, "xtls-rprx-vision", nil); err != nil {
		t.Fatalf("BulkAdjust: %v", err)
	}

	stamps := settingsUpdatedAt(t, inboundSvc, ib.Id)
	if stamps["chg@x"] <= seeded {
		t.Fatalf("adjusted client should be re-stamped, updated_at = %d", stamps["chg@x"])
	}
	if stamps["keep@x"] != seeded {
		t.Fatalf("untouched client updated_at = %d, want %d — a sibling's change must not re-stamp it", stamps["keep@x"], seeded)
	}
}

func settingsUpdatedAt(t *testing.T, inboundSvc *InboundService, inboundId int) map[string]int64 {
	t.Helper()
	ib, err := inboundSvc.GetInbound(inboundId)
	if err != nil {
		t.Fatalf("GetInbound: %v", err)
	}
	var parsed struct {
		Clients []struct {
			Email     string `json:"email"`
			UpdatedAt int64  `json:"updated_at"`
		} `json:"clients"`
	}
	if err := json.Unmarshal([]byte(ib.Settings), &parsed); err != nil {
		t.Fatalf("unmarshal settings: %v", err)
	}
	out := make(map[string]int64, len(parsed.Clients))
	for _, c := range parsed.Clients {
		out[c.Email] = c.UpdatedAt
	}
	return out
}
