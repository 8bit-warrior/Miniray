package database

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/8bit-warrior/Miniray/internal/database/model"
	"github.com/8bit-warrior/Miniray/internal/util/crypto"
)

func TestInitDBGeneratesPanelAccess(t *testing.T) {
	t.Setenv("MINIRAY_IN_DOCKER", "false")
	dbPath := filepath.Join(t.TempDir(), "miniray.db")
	if err := InitDB(dbPath); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = CloseDB() }()

	credentialsFile := filepath.Join(filepath.Dir(dbPath), "initial-admin-credentials")
	credentials, err := os.ReadFile(credentialsFile)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(strings.TrimSpace(string(credentials)), "\n")
	if len(parts) != 2 || !regexp.MustCompile("^[0-9a-zA-Z]{8,10}$").MatchString(parts[0]) || !regexp.MustCompile("^[0-9a-zA-Z]{10,12}$").MatchString(parts[1]) {
		t.Fatalf("invalid generated credentials: %q", parts)
	}
	fileInfo, err := os.Stat(credentialsFile)
	if err != nil || fileInfo.Mode().Perm() != 0o600 {
		t.Fatalf("initial credentials permissions: %v, %v", fileInfo, err)
	}
	var user model.User
	if err := db.First(&user).Error; err != nil {
		t.Fatal(err)
	}
	if user.Username != parts[0] || !crypto.CheckPasswordHash(user.Password, parts[1]) {
		t.Fatal("generated credentials do not match persisted user")
	}
	settings := make(map[string]string)
	for _, key := range []string{"webPort", "webBasePath"} {
		var setting model.Setting
		if err := db.Where("key = ?", key).First(&setting).Error; err != nil {
			t.Fatal(err)
		}
		settings[key] = setting.Value
	}
	port, err := strconv.Atoi(settings["webPort"])
	if err != nil || port < 20000 || port > 65535 {
		t.Fatalf("port not a high port: %q", settings["webPort"])
	}
	if !regexp.MustCompile("^/[0-9a-z]{8,10}/$").MatchString(settings["webBasePath"]) {
		t.Fatalf("unexpected web path: %q", settings["webBasePath"])
	}
	if err := InitDB(dbPath); err != nil {
		t.Fatal(err)
	}
	var secondUser model.User
	if err := db.First(&secondUser).Error; err != nil || secondUser.Username != user.Username {
		t.Fatalf("reinitialization replaced the existing user: %v", err)
	}
	for key, original := range settings {
		var setting model.Setting
		if err := db.Where("key = ?", key).First(&setting).Error; err != nil || setting.Value != original {
			t.Fatalf("%s changed on reinitialization: %v", key, err)
		}
	}
}
