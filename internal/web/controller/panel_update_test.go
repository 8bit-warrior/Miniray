package controller

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/8bit-warrior/Miniray/internal/config"

	"github.com/gin-gonic/gin"
)

// Register only the update handlers, without starting cron or metrics jobs.
func newPanelUpdateTestEngine() *gin.Engine {
	a := &ServerController{}
	engine := gin.New()
	engine.GET("/panel/api/server/getUpdateStatus", a.getUpdateStatus)
	engine.POST("/panel/api/server/updatePanel", a.updatePanel)
	return engine
}

func doPanelUpdateReq(t *testing.T, engine *gin.Engine, method, path string) controllerEnvelope {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("%s %s: status %d, body=%s", method, path, w.Code, w.Body.String())
	}
	var env controllerEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("%s %s: decode envelope: %v body=%s", method, path, err, w.Body.String())
	}
	return env
}

// TestGetUpdateStatus_NoStatusFileYet exercises the read-only status endpoint
// with no prior update having run: it must report "pending" (not an error),
// since a missing status file is an expected, ordinary state, not a failure.
func TestGetUpdateStatus_NoStatusFileYet(t *testing.T) {
	newControllerTestDB(t)
	engine := newPanelUpdateTestEngine()

	env := doPanelUpdateReq(t, engine, http.MethodGet, "/panel/api/server/getUpdateStatus")
	if !env.Success {
		t.Fatalf("getUpdateStatus should always report success=true (it's a best-effort read): msg=%s", env.Msg)
	}
	var status struct {
		RunID string `json:"runId"`
		State string `json:"state"`
	}
	if err := json.Unmarshal(env.Obj, &status); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if status.State != "pending" {
		t.Fatalf("State = %q, want %q", status.State, "pending")
	}
}

// TestGetUpdateStatus_RunIdIsAlwaysAString is the regression test for the
// precision bug found in review: RunID is a 19-digit UnixNano timestamp, so
// it must round-trip over the wire as a JSON string, never a bare number -- a
// bare number would silently lose precision in JavaScript past
// Number.MAX_SAFE_INTEGER, breaking every future runId comparison on the
// frontend. Decoding into a Go string field below only succeeds if the wire
// value is actually a JSON string; a bare number there would fail to decode,
// so this test doubles as the wire-format check.
func TestGetUpdateStatus_RunIdIsAlwaysAString(t *testing.T) {
	newControllerTestDB(t)
	engine := newPanelUpdateTestEngine()

	statusPath := config.GetUpdateStatusFilePath()
	body := `{"runId":"1735689600123456789","state":"success","exitCode":0,"finishedAt":1735689612}`
	if err := os.WriteFile(statusPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	env := doPanelUpdateReq(t, engine, http.MethodGet, "/panel/api/server/getUpdateStatus")
	var status struct {
		RunID string `json:"runId"`
		State string `json:"state"`
	}
	if err := json.Unmarshal(env.Obj, &status); err != nil {
		t.Fatalf("decode status (would fail here if runId were a bare JSON number instead of a string): %v, body=%s", err, env.Obj)
	}
	if status.RunID != "1735689600123456789" {
		t.Fatalf("RunID = %q, want %q", status.RunID, "1735689600123456789")
	}
	if status.State != "success" {
		t.Fatalf("State = %q, want %q", status.State, "success")
	}
}

// TestUpdatePanel_UnsupportedPlatformReturnsNoRunId covers the one path of
// updatePanel that's safe to exercise in an automated test on any OS/CI
// runner: the runtime.GOOS != "linux" guard. Actually invoking StartUpdate's
// launch logic on Linux would make a real network call and could launch a
// real update.sh process, so that path is deliberately not covered here --
// see the PR description for why.
func TestUpdatePanel_UnsupportedPlatformReturnsNoRunId(t *testing.T) {
	if runtime.GOOS == "linux" {
		t.Skip("this test only exercises the non-Linux guard path; on Linux, updatePanel would attempt a real download/exec")
	}
	newControllerTestDB(t)
	engine := newPanelUpdateTestEngine()

	env := doPanelUpdateReq(t, engine, http.MethodPost, "/panel/api/server/updatePanel")
	if env.Success {
		t.Fatal("updatePanel on an unsupported platform: success = true, want false")
	}
	if len(env.Obj) != 0 && string(env.Obj) != "null" {
		t.Fatalf("updatePanel error response must not carry an obj/runId: got %s", env.Obj)
	}
}

func TestUpdatePanel_InvalidActionRunIDRejectedBeforeLaunch(t *testing.T) {
	newControllerTestDB(t)
	engine := newPanelUpdateTestEngine()

	form := url.Values{"actionRunId": {"not-a-run"}}
	req := httptest.NewRequest(http.MethodPost, "/panel/api/server/updatePanel", strings.NewReader(form.Encode()))
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
	if env.Success {
		t.Fatal("updatePanel with invalid actionRunId: success = true, want false")
	}
	if !strings.Contains(env.Msg, "invalid Actions run ID") {
		t.Fatalf("updatePanel error = %q, want invalid Actions run ID", env.Msg)
	}
	if len(env.Obj) != 0 && string(env.Obj) != "null" {
		t.Fatalf("updatePanel error response must not carry an obj/runId: got %s", env.Obj)
	}
}
