package controller

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/8bit-warrior/Miniray/internal/database/dbtest"
	"github.com/8bit-warrior/Miniray/internal/web/locale"
	"github.com/8bit-warrior/Miniray/internal/web/service"
)

func TestValidateRegex(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	NewSettingController(router.Group("/panel/api"))

	tests := []struct {
		name    string
		body    string
		success bool
	}{
		{name: "Go RE2 inline flag", body: `{"regex":"(?m)^general-purpose$"}`, success: true},
		{name: "invalid expression", body: `{"regex":"["}`, success: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/panel/api/setting/validateRegex", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			resp := httptest.NewRecorder()

			router.ServeHTTP(resp, req)

			if resp.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body=%s", resp.Code, resp.Body.String())
			}
			needle := `"success":true`
			if !tt.success {
				needle = `"success":false`
			}
			if !strings.Contains(resp.Body.String(), needle) {
				t.Fatalf("body = %s, want %s", resp.Body.String(), needle)
			}
		})
	}
}

// GHSA-xqqw-jqqv-99h6: a save that keeps 2FA enabled must not be able to
// rebind the authenticator without presenting a current code.
func TestUpdateSettingRequiresCodeToReplaceTwoFactorToken(t *testing.T) {
	t.Setenv("MINIRAY_DB_FOLDER", t.TempDir())
	dbtest.InitDB(t, filepath.Join(t.TempDir(), "miniray.db"))

	settingService := service.SettingService{}
	if err := settingService.SetTwoFactorToken("ORIGINALSECRET234567"); err != nil {
		t.Fatalf("seed token: %v", err)
	}
	if err := settingService.SetTwoFactorEnable(true); err != nil {
		t.Fatalf("seed enable: %v", err)
	}

	post := func(t *testing.T, mutate func(map[string]any)) string {
		t.Helper()
		base, err := settingService.GetAllSetting()
		if err != nil {
			t.Fatalf("GetAllSetting: %v", err)
		}
		raw, err := json.Marshal(base)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		body := map[string]any{}
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		mutate(body)
		payload, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal payload: %v", err)
		}

		gin.SetMode(gin.TestMode)
		router := gin.New()
		NewSettingController(router.Group("/panel/api"))
		req := httptest.NewRequest(http.MethodPost, "/panel/api/setting/update", strings.NewReader(string(payload)))
		req.Header.Set("Content-Type", "application/json")
		resp := httptest.NewRecorder()
		router.ServeHTTP(resp, req)
		return resp.Body.String()
	}

	t.Run("rebind without code is rejected", func(t *testing.T) {
		got := post(t, func(body map[string]any) {
			body["twoFactorEnable"] = true
			body["twoFactorToken"] = "ATTACKERSECRET567890"
		})
		if !strings.Contains(got, `"success":false`) {
			t.Fatalf("rebind without a 2FA code was accepted: %s", got)
		}
		stored, err := settingService.GetTwoFactorToken()
		if err != nil {
			t.Fatalf("GetTwoFactorToken: %v", err)
		}
		if stored != "ORIGINALSECRET234567" {
			t.Fatalf("stored 2FA secret = %q, want it unchanged", stored)
		}
	})

	t.Run("ordinary save with redacted token still succeeds", func(t *testing.T) {
		got := post(t, func(body map[string]any) {
			body["twoFactorEnable"] = true
			body["twoFactorToken"] = ""
		})
		if !strings.Contains(got, `"success":true`) {
			t.Fatalf("normal settings save was rejected: %s", got)
		}
		stored, err := settingService.GetTwoFactorToken()
		if err != nil {
			t.Fatalf("GetTwoFactorToken: %v", err)
		}
		if stored != "ORIGINALSECRET234567" {
			t.Fatalf("stored 2FA secret = %q, want it preserved", stored)
		}
	})
}

