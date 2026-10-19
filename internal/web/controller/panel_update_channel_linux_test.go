package controller

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/8bit-warrior/Miniray/internal/config"
)

type panelUpdateTransport func(*http.Request) (*http.Response, error)

func (f panelUpdateTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestUpdatePanel_StableChannelOverridesInstalledDevelopmentChannel(t *testing.T) {
	newControllerTestDB(t)
	t.Chdir(t.TempDir())
	commit := strings.Repeat("a", 40)
	if err := os.WriteFile(".action-commit", []byte(commit), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := config.GetBuildCommit(); got != commit {
		t.Fatalf("installed development commit = %q, want %q", got, commit)
	}
	engine := newPanelUpdateTestEngine()

	for _, tc := range []struct {
		name      string
		form      url.Values
		wantURL   string
		wantError string
	}{
		{
			name:      "explicit stable bypasses development builds",
			form:      url.Values{"channel": {"stable"}},
			wantURL:   "https://raw.githubusercontent.com/8bit-warrior/Miniray/main/update.sh",
			wantError: "download panel updater: unexpected HTTP 503",
		},
		{
			name:      "unspecified channel still follows development builds",
			wantURL:   "https://api.github.com/repos/8bit-warrior/Miniray/actions/workflows/release.yml/runs?branch=main&status=success&per_page=100",
			wantError: "GitHub Actions returned HTTP 503",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			originalTransport := http.DefaultTransport
			t.Cleanup(func() { http.DefaultTransport = originalTransport })
			var requestedURLs []string
			http.DefaultTransport = panelUpdateTransport(func(req *http.Request) (*http.Response, error) {
				requestedURLs = append(requestedURLs, req.URL.String())
				return &http.Response{
					StatusCode: http.StatusServiceUnavailable,
					Body:       http.NoBody,
				}, nil
			})

			req := httptest.NewRequest(http.MethodPost, "/panel/api/server/updatePanel", strings.NewReader(tc.form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			w := httptest.NewRecorder()
			engine.ServeHTTP(w, req)
			if w.Code != http.StatusOK {
				t.Fatalf("status %d, body=%s", w.Code, w.Body.String())
			}
			var env controllerEnvelope
			if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
				t.Fatalf("decode envelope: %v body=%s", err, w.Body.String())
			}
			if env.Success || !strings.Contains(env.Msg, tc.wantError) {
				t.Fatalf("update response = %+v, want failure containing %q", env, tc.wantError)
			}
			if len(requestedURLs) != 1 || requestedURLs[0] != tc.wantURL {
				t.Fatalf("update requests = %v, want [%s]", requestedURLs, tc.wantURL)
			}
		})
	}
}
