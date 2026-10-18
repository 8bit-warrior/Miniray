package panel

import (
	"encoding/json"
	"net/http"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDevelopmentBuildOrder(t *testing.T) {
	prevBase, prevClient := actionsAPIBase, actionsClient
	defer func() { actionsAPIBase, actionsClient = prevBase, prevClient }()
	actionsAPIBase = ""
	actionsClient = func() *http.Client {
		return &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			status := "ahead"
			if strings.HasPrefix(req.URL.Path, "/compare/") {
				status = "ahead"
			}
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"status":"`+status+`","ahead_by":1}`)), Header: make(http.Header)}, nil
		})}
	}
	newest := strings.Repeat("b", 40)
	older := strings.Repeat("a", 40)
	builds := []ActionBuild{{Commit: newest[:8], FullSHA: newest}, {Commit: older[:8], FullSHA: older}}
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

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestDevelopmentBuildAncestryWhenArtifactExpired(t *testing.T) {
	current := strings.Repeat("a", 40)
	latest := strings.Repeat("b", 40)
	for _, tc := range []struct { status string; want bool }{
		{"ahead", true}, {"behind", false}, {"identical", false}, {"diverged", false},
	} {
		t.Run(tc.status, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/compare/"+current+"..."+latest[:8] { t.Errorf("unexpected path %s", r.URL.Path) }
				ahead := 0
				if tc.status == "ahead" { ahead = 1 }
				_ = json.NewEncoder(w).Encode(map[string]any{"status": tc.status, "ahead_by": ahead})
			}))
			defer server.Close()
			prevBase, prevClient := actionsAPIBase, actionsClient
			actionsAPIBase = server.URL
			actionsClient = func() *http.Client { return server.Client() }
			defer func(){ actionsAPIBase, actionsClient = prevBase, prevClient }()
			got, err := isDevelopmentBuildNewer([]ActionBuild{{Commit: latest[:8], FullSHA: latest}}, current)
			if err != nil { t.Fatal(err) }
			if got != tc.want { t.Fatalf("status %s: got %v, want %v", tc.status, got, tc.want) }
		})
	}
}
