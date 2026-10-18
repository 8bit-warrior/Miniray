package service

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/8bit-warrior/Miniray/internal/database/dbtest"
)

func TestValidObsTag(t *testing.T) {
	tests := []struct {
		name string
		tag  string
		want bool
	}{
		{"plain ascii", "proxy-1", true},
		{"dots and underscores", "warp_us.east", true},
		{"flag emoji", "🇩🇪 Germany", true},
		{"cyrillic", "Германия", true},
		{"spaces allowed", "US proxy 2", true},
		{"empty rejected", "", false},
		{"control char rejected", "bad\x00tag", false},
		{"newline rejected", "bad\ntag", false},
		{"invalid utf8 rejected", string([]byte{0xff, 0xfe}), false},
		{"overlong rejected", strings.Repeat("a", maxObsTagLength+1), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := validObsTag(tc.tag); got != tc.want {
				t.Fatalf("validObsTag(%q) = %v, want %v", tc.tag, got, tc.want)
			}
		})
	}
}

func TestApplyObservatoryKeepsUnicodeTags(t *testing.T) {
	dbDir := t.TempDir()
	dbtest.InitDB(t, filepath.Join(dbDir, "miniray.db"))

	s := &XrayMetricsService{settingService: SettingService{}}
	s.applyObservatory(time.Unix(1000, 0), map[string]rawObsEntry{
		"🇩🇪 Berlin": {Alive: true, Delay: 42, LastTryTime: 1},
	})

	if !s.HasObservatoryTag("🇩🇪 Berlin") {
		t.Fatal("emoji-tagged outbound must appear in the observatory")
	}
	snaps := s.ObservatorySnapshot()
	if len(snaps) != 1 || snaps[0].Tag != "🇩🇪 Berlin" {
		t.Fatalf("snapshot = %+v, want the emoji tag", snaps)
	}
}
