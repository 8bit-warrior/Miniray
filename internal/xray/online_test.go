package xray

import (
	"slices"
	"testing"
)

func newOnlineTestProcess() *Process {
	return &Process{newProcess(nil)}
}

func assertSameSet(t *testing.T, label string, got, want []string) {
	t.Helper()
	g := append([]string(nil), got...)
	w := append([]string(nil), want...)
	slices.Sort(g)
	slices.Sort(w)
	if !slices.Equal(g, w) {
		t.Errorf("%s = %v, want %v", label, got, want)
	}
}

// TestRefreshLocalOnlineGraceWindow checks the in-memory local set honours the
// grace window: idle-but-recent clients stay online, stale ones age out, and
// the set is derived only from local activity (never the shared DB column).
func TestRefreshLocalOnlineGraceWindow(t *testing.T) {
	p := newOnlineTestProcess()
	const grace = 20000

	p.RefreshLocalOnline([]string{"user1"}, nil, 1000, grace)
	if got := p.GetLocalOnlineClients(); !slices.Contains(got, "user1") {
		t.Fatalf("user1 should be online right after activity, got %v", got)
	}

	p.RefreshLocalOnline([]string{"user2"}, nil, 11000, grace)
	got := p.GetLocalOnlineClients()
	if !slices.Contains(got, "user1") || !slices.Contains(got, "user2") {
		t.Fatalf("both within grace window, got %v", got)
	}

	p.RefreshLocalOnline(nil, nil, 22000, grace)
	got = p.GetLocalOnlineClients()
	if slices.Contains(got, "user1") {
		t.Errorf("user1 (idle 21s, past grace) should have aged out, got %v", got)
	}
	if !slices.Contains(got, "user2") {
		t.Errorf("user2 (idle 11s, within grace) should still be online, got %v", got)
	}
}

// TestGetLocalActiveInboundsTracksGraceWindow pins #4859: a multi-inbound
// client only counts online on inbounds that actually carried traffic, and the
// active-inbound signal honours the same grace window as the online signal.
func TestGetLocalActiveInboundsTracksGraceWindow(t *testing.T) {
	p := newOnlineTestProcess()
	const grace = 20000

	p.RefreshLocalOnline([]string{"alice"}, []string{"inbound-a"}, 1000, grace)
	assertSameSet(t, "active after first poll", p.GetLocalActiveInbounds(), []string{"inbound-a"})

	p.RefreshLocalOnline([]string{"alice"}, []string{"inbound-b"}, 11000, grace)
	assertSameSet(t, "both within grace", p.GetLocalActiveInbounds(), []string{"inbound-a", "inbound-b"})

	p.RefreshLocalOnline(nil, nil, 22000, grace)
	assertSameSet(t, "inbound-a (idle 21s) aged out, inbound-b kept", p.GetLocalActiveInbounds(), []string{"inbound-b"})

	p.RefreshLocalOnline(nil, nil, 40000, grace)
	if got := p.GetLocalActiveInbounds(); len(got) != 0 {
		t.Errorf("all inbounds idle past grace, want empty, got %v", got)
	}
}

// TestOnlineAPISupportTriState pins the lazy capability probe contract: a new
// process starts Unknown (so the first caller probes), and the flag holds
// whatever the probe recorded until the process is replaced on restart.
func TestOnlineAPISupportTriState(t *testing.T) {
	p := newOnlineTestProcess()
	if got := p.OnlineAPISupport(); got != OnlineAPIUnknown {
		t.Fatalf("new process must start with OnlineAPIUnknown, got %v", got)
	}
	p.SetOnlineAPISupport(OnlineAPISupported)
	if got := p.OnlineAPISupport(); got != OnlineAPISupported {
		t.Fatalf("expected OnlineAPISupported, got %v", got)
	}
	p.SetOnlineAPISupport(OnlineAPIUnsupported)
	if got := p.OnlineAPISupport(); got != OnlineAPIUnsupported {
		t.Fatalf("expected OnlineAPIUnsupported, got %v", got)
	}
}
