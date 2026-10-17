package panel

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
)

func TestListActionBuildsSuccessOrderAndArtifactEligibility(t *testing.T) {
	shaOld := strings.Repeat("a", 40)
	shaNew := strings.Repeat("b", 40)
	shaOther := strings.Repeat("c", 40)
	digest := "sha256:" + strings.Repeat("f", 64)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/actions/workflows/release.yml/runs":
			if r.URL.Query().Get("branch") != "main" || r.URL.Query().Get("status") != "success" {
				t.Errorf("unexpected run filters: %s", r.URL.RawQuery)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"workflow_runs": []map[string]any{
					{"id": 101, "head_sha": shaOld, "head_branch": "main", "status": "completed", "conclusion": "success", "created_at": "2026-10-15T12:00:00Z", "display_title": "older change"},
					{"id": 103, "head_sha": shaOther, "head_branch": "main", "status": "completed", "conclusion": "success", "created_at": "2026-10-17T12:00:00Z", "display_title": "expired change"},
					{"id": 102, "head_sha": shaNew, "head_branch": "main", "status": "completed", "conclusion": "success", "created_at": "2026-10-16T12:00:00Z", "display_title": "newer change"},
					{"id": 104, "head_sha": shaOther, "head_branch": "main", "status": "completed", "conclusion": "success", "created_at": "2026-10-18T12:00:00Z", "display_title": "mismatched artifact"},
				},
			})
		case "/actions/artifacts":
			if r.URL.Query().Get("name") != "miniray-linux-"+runtime.GOARCH {
				t.Errorf("unexpected artifact name: %s", r.URL.RawQuery)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"artifacts": []map[string]any{
					{"id": 1, "name": "miniray-linux-" + runtime.GOARCH, "digest": digest, "expired": false, "workflow_run": map[string]any{"id": 101, "head_sha": shaOld}},
					{"id": 2, "name": "miniray-linux-" + runtime.GOARCH, "digest": digest, "expired": false, "workflow_run": map[string]any{"id": 102, "head_sha": shaNew}},
					{"id": 3, "name": "miniray-linux-" + runtime.GOARCH, "digest": digest, "expired": true, "workflow_run": map[string]any{"id": 103, "head_sha": shaOther}},
					{"id": 4, "name": "miniray-linux-" + runtime.GOARCH, "digest": digest, "expired": false, "workflow_run": map[string]any{"id": 104, "head_sha": shaNew}},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	prevBase, prevClient := actionsAPIBase, actionsClient
	actionsAPIBase = server.URL
	actionsClient = func() *http.Client { return server.Client() }
	t.Cleanup(func() {
		actionsAPIBase = prevBase
		actionsClient = prevClient
	})

	s := &PanelService{}
	builds, err := s.ListActionBuilds()
	if err != nil {
		t.Fatal(err)
	}
	if len(builds) != 2 || builds[0].RunID != "102" || builds[1].RunID != "101" {
		t.Fatalf("unexpected builds: %+v", builds)
	}
	if builds[0].Version != "dev+"+shaNew[:8] || builds[0].Title != "newer change" {
		t.Fatalf("unexpected build identity: %+v", builds[0])
	}
	if _, err := s.StartUpdateBuild("104"); err == nil {
		t.Fatal("rejected build was accepted for installation")
	}
	if _, err := s.StartUpdateBuild("invalid"); err == nil {
		t.Fatal("invalid run ID was accepted")
	}
}
