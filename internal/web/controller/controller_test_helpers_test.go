package controller

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/8bit-warrior/Miniray/internal/database/dbtest"
)

type controllerEnvelope struct {
	Success bool            `json:"success"`
	Msg     string          `json:"msg"`
	Obj     json.RawMessage `json:"obj"`
}

func newControllerTestDB(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("MINIRAY_DB_FOLDER", dir)
	dbtest.InitDB(t, filepath.Join(dir, "miniray.db"))
}
