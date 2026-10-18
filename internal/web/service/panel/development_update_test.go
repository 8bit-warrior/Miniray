package panel

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDevelopmentBuildOrder(t *testing.T) {
	newest := strings.Repeat("b", 40)
	older := strings.Repeat("a", 40)
	builds := []ActionBuild{{Commit: newest[:8]}, {Commit: older[:8]}}
	for _, tc := range []struct {
		name string
		current string
		want bool
	}{
		{"latest is installed", newest, false},
		{"older successful build is installed", older, true},
		{"unknown build identity", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := isDevelopmentBuildNewer(builds, tc.current)
			if err != nil { t.Fatal(err) }
			if got != tc.want { t.Fatalf("got %v; want %v", got, tc.want) }
		})
	}
}

func TestDevelopmentBuildAncestryWhenArtifactExpired(t *testing.T) {
	current := strings.Repeat("a", 40)
	latest := strings.Repeat("b", 40)
	for _, tc := range []struct { status string; want bool }{
		{"ahead", true}, {"behind", false}, {"identical", false}, {"diverged", false},
	} {
		t.Run(tc.status, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/compare/"+current+"..."+latest[:8] { t.Errorf("unexpected path %s", r.URL.Path) }
				_ = json.NewEncoder(w).Encode(map[string]string{"status": tc.status})
			}))
			defer server.Close()
			prevBase, prevClient := actionsAPIBase, actionsClient
			actionsAPIBase = server.URL
			actionsClient = func() *http.Client { return server.Client() }
			defer func(){ actionsAPIBase, actionsClient = prevBase, prevClient }()
			got, err := isDevelopmentBuildNewer([]ActionBuild{{Commit: latest[:8]}}, current)
			if err != nil { t.Fatal(err) }
			if got != tc.want { t.Fatalf("status %s: got %v, want %v", tc.status, got, tc.want) }
		})
	}
}
