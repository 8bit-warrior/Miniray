//go:build linux

package mtproto

import "testing"

func TestOwnedMtgCommandIdentity(t *testing.T) {
	binary := "bin/mtg-linux-amd64"
	cases := []struct {
		name  string
		args  string
		owned bool
	}{
		{"owned", "bin/mtg-linux-amd64\x00run\x00bin/mtproto/mtg-15.toml\x00", true},
		{"unrelated program", "bin/mtg-linux-amd64\x00120\x00", false},
		{"external config", "bin/mtg-linux-amd64\x00run\x00other/mtproto/mtg-15.toml\x00", false},
		{"another command", "bin/mtg-linux-amd64\x00version\x00bin/mtproto/mtg-15.toml\x00", false},
		{"non-numeric config", "bin/mtg-linux-amd64\x00run\x00bin/mtproto/mtg-not-owned.toml\x00", false},
		{"extra arguments", "bin/mtg-linux-amd64\x00run\x00bin/mtproto/mtg-15.toml\x00--debug\x00", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isOwnedMtgCommand([]byte(tc.args), binary); got != tc.owned {
				t.Errorf("owned = %v, want %v", got, tc.owned)
			}
		})
	}
}
