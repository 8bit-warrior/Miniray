package service

import (
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/xlzd/gotp"

	"github.com/8bit-warrior/Miniray/internal/database"
	"github.com/8bit-warrior/Miniray/internal/database/dbtest"
	"github.com/8bit-warrior/Miniray/internal/database/model"
)

func TestResetSettingsRegeneratesSubscriptionPaths(t *testing.T) {
	setupSettingTestDB(t)
	s := &SettingService{}
	for key, value := range map[string]string{
		"subPath":      "/sub/",
		"subJsonPath":  "/json/",
		"subClashPath": "/clash/",
		"webPort":      "8443",
	} {
		if err := s.saveSetting(key, value); err != nil {
			t.Fatalf("save %s: %v", key, err)
		}
	}

	if err := s.ResetSettings(); err != nil {
		t.Fatalf("ResetSettings: %v", err)
	}

	pathPattern := regexp.MustCompile(`^/[0-9a-z]{16}/$`)
	paths := map[string]string{}
	for _, key := range []string{"subPath", "subJsonPath", "subClashPath"} {
		value, err := s.getString(key)
		if err != nil {
			t.Fatalf("read %s: %v", key, err)
		}
		if !pathPattern.MatchString(value) {
			t.Errorf("%s = %q, want /<16 lowercase alphanumeric characters>/", key, value)
		}
		paths[key] = value
	}
	if paths["subPath"] == paths["subJsonPath"] || paths["subPath"] == paths["subClashPath"] || paths["subJsonPath"] == paths["subClashPath"] {
		t.Fatalf("subscription paths must be distinct: %v", paths)
	}
	if port, err := s.GetPort(); err != nil || port != 2053 {
		t.Fatalf("web port after reset = %d, %v; want 2053", port, err)
	}
}

func setupSettingTestDB(t *testing.T) {
	t.Helper()
	dbtest.InitDB(t, filepath.Join(t.TempDir(), "miniray.db"))
}

func TestGetAllSettingViewRedactsSecrets(t *testing.T) {
	setupSettingTestDB(t)
	s := &SettingService{}
	for key, value := range map[string]string{
		"twoFactorToken": "totp-secret",
		"ldapPassword":   "ldap-secret",
	} {
		if err := s.saveSetting(key, value); err != nil {
			t.Fatal(err)
		}
	}
	if err := database.GetDB().Create(&model.ApiToken{Name: "test", Token: "api-secret", Enabled: true}).Error; err != nil {
		t.Fatal(err)
	}
	view, err := s.GetAllSettingView()
	if err != nil {
		t.Fatal(err)
	}
	if view.TwoFactorToken != "" || view.LdapPassword != "" {
		t.Fatal("settings view leaked secrets")
	}
	if !view.HasTwoFactorToken || !view.HasLdapPassword || !view.HasApiToken {
		t.Fatal("settings view omitted secret flags")
	}
}

func TestUpdateAllSettingPreservesRedactedSecrets(t *testing.T) {
	setupSettingTestDB(t)
	s := &SettingService{}
	for key, value := range map[string]string{
		"ldapPassword":    "ldap-secret",
		"twoFactorEnable": "true",
		"twoFactorToken":  "totp-secret",
	} {
		if err := s.saveSetting(key, value); err != nil {
			t.Fatal(err)
		}
	}
	view, err := s.GetAllSettingView()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateAllSetting(&view.AllSetting, SecretClears{}); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetLdapPassword(); got != "ldap-secret" {
		t.Fatalf("ldap password %q", got)
	}
	if got, _ := s.GetTwoFactorToken(); got != "totp-secret" {
		t.Fatalf("two-factor token %q", got)
	}
}

func TestUpdateAllSettingClearsFlaggedSecrets(t *testing.T) {
	setupSettingTestDB(t)
	s := &SettingService{}
	if err := s.saveSetting("ldapPassword", "ldap-secret"); err != nil {
		t.Fatal(err)
	}
	view, err := s.GetAllSettingView()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateAllSetting(&view.AllSetting, SecretClears{LdapPassword: true}); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetLdapPassword(); got != "" {
		t.Fatalf("ldap password %q, want cleared", got)
	}
	view, err = s.GetAllSettingView()
	if err != nil {
		t.Fatal(err)
	}
	if view.HasLdapPassword {
		t.Fatal("cleared LDAP secret is still marked present")
	}
}

func TestSanitizePublicHTTPURLBlocksPrivateAddressUnlessAllowed(t *testing.T) {
	if _, err := SanitizePublicHTTPURL("http://127.0.0.1:8080/hook", false); err == nil {
		t.Fatal("expected localhost URL to be blocked")
	}
	if got, err := SanitizePublicHTTPURL("http://127.0.0.1:8080/hook", true); err != nil || got != "http://127.0.0.1:8080/hook" {
		t.Fatalf("allowPrivate result = %q, %v", got, err)
	}
}

func TestVerifyTwoFactorCode(t *testing.T) {
	setupSettingTestDB(t)
	s := &SettingService{}
	if err := s.saveSetting("twoFactorEnable", "true"); err != nil {
		t.Fatal(err)
	}
	const token = "JBSWY3DPEHPK3PXP"
	if err := s.saveSetting("twoFactorToken", token); err != nil {
		t.Fatal(err)
	}

	if err := s.VerifyTwoFactorCode(gotp.NewDefaultTOTP(token).Now()); err != nil {
		t.Fatalf("valid code rejected: %v", err)
	}
	if err := s.VerifyTwoFactorCode(gotp.NewDefaultTOTP(token).AtTime(time.Now().Add(-30 * time.Second))); err != nil {
		t.Fatalf("previous window code rejected: %v", err)
	}
	if err := s.VerifyTwoFactorCode("000000"); err == nil {
		t.Fatal("invalid code accepted")
	}
}

func TestGetSecret_FallbacksOnEmptyDatabaseSetting(t *testing.T) {
	setupSettingTestDB(t)
	s := &SettingService{}
	if err := s.saveSetting("secret", ""); err != nil {
		t.Fatal(err)
	}

	secretBytes, err := s.GetSecret()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	secret := string(secretBytes)
	if len(secret) != 32 {
		t.Fatalf("secret length = %d, want 32-byte non-empty string", len(secret))
	}

	persisted, err := s.getString("secret")
	if err != nil {
		t.Fatalf("failed to retrieve persisted secret: %v", err)
	}
	if persisted != secret {
		t.Fatalf("persisted secret = %q, want %q", persisted, secret)
	}
}
