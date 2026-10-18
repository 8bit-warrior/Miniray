// Package config provides configuration management utilities for the miniray panel,
// including version information, logging levels, database paths, and environment variable handling.
package config

import (
	_ "embed"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

//go:embed version
var version string

//go:embed name
var name string

// buildCommit and buildDate are injected at build time via `-ldflags -X` for
// CI per-commit (dev channel) builds; see .github/workflows/release.yml. They
// stay empty for a plain `go build` and for stable tagged releases, which is how
// IsDevBuild tells a rolling dev build apart from a stable/local one.
var (
	buildCommit string
	buildDate   string
)

// LogLevel represents the logging level for the application.
type LogLevel string

// Logging level constants
const (
	Debug   LogLevel = "debug"
	Info    LogLevel = "info"
	Notice  LogLevel = "notice"
	Warning LogLevel = "warning"
	Error   LogLevel = "error"
)

// GetBaseVersion returns the raw embedded release version of the miniray panel
// (e.g. "3.4.0"). This is the panel's own version, not the Xray version.
func GetBaseVersion() string {
	return strings.TrimSpace(version)
}

// GetName returns the name of the miniray application.
func GetName() string {
	return strings.TrimSpace(name)
}

// GetBuildCommit returns the short git commit this binary was built from, or an
// empty string for a plain/local build or a stable tagged release.
func GetBuildCommit() string {
	if value := strings.TrimSpace(buildCommit); value != "" {
		return value
	}
	if runtime.GOOS != "linux" {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(getBaseDir(), ".action-commit"))
	if err != nil {
		return ""
	}
	commit := strings.TrimSpace(string(data))
	if len(commit) != 40 {
		return ""
	}
	if _, err := hex.DecodeString(commit); err != nil {
		return ""
	}
	return strings.ToLower(commit)
}

// GetBuildDate returns the UTC build timestamp injected at build time, or empty.
func GetBuildDate() string {
	return strings.TrimSpace(buildDate)
}

// IsDevBuild reports whether this binary is a CI per-commit (dev channel) build,
// detected by the injected commit. Stable releases and local builds return false.
func IsDevBuild() bool {
	return GetBuildCommit() != ""
}

func GetPanelVersion() string {
	commit := GetBuildCommit()
	if commit == "" {
		return GetBaseVersion()
	}
	if len(commit) > 7 {
		commit = commit[:7]
	}
	return GetBaseVersion() + "_" + strings.ToLower(commit)
}

// GetLogLevel returns the current logging level based on environment variables or defaults to Info.
func GetLogLevel() LogLevel {
	if IsDebug() {
		return Debug
	}
	logLevel := os.Getenv("MINIRAY_LOG_LEVEL")
	if logLevel == "" {
		return Info
	}
	return LogLevel(logLevel)
}

// IsDebug returns true if debug mode is enabled via the MINIRAY_DEBUG environment variable.
func IsDebug() bool {
	return os.Getenv("MINIRAY_DEBUG") == "true"
}

// IsSkipHSTS returns true if skipping HSTS mode is enabled via the MINIRAY_SKIP_HSTS environment variable.
func IsSkipHSTS() bool {
	return os.Getenv("MINIRAY_SKIP_HSTS") == "true"
}

func GetPortOverride() (port int, configured bool, err error) {
	value, ok := os.LookupEnv("MINIRAY_PORT")
	if !ok || strings.TrimSpace(value) == "" {
		return 0, false, nil
	}

	port, err = strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return 0, true, fmt.Errorf("parse MINIRAY_PORT: %w", err)
	}
	if port < 1 || port > 65535 {
		return 0, true, fmt.Errorf("MINIRAY_PORT must be between 1 and 65535")
	}

	return port, true, nil
}

// GetBinFolderPath returns the path to the binary folder, defaulting to "bin" if not set via MINIRAY_BIN_FOLDER.
func GetBinFolderPath() string {
	binFolderPath := os.Getenv("MINIRAY_BIN_FOLDER")
	if binFolderPath == "" {
		binFolderPath = "bin"
	}
	return binFolderPath
}

func getBaseDir() string {
	exePath, err := os.Executable()
	if err != nil {
		return "."
	}
	exeDir := filepath.Dir(exePath)
	exeDirLower := strings.ToLower(filepath.ToSlash(exeDir))
	if strings.Contains(exeDirLower, "/appdata/local/temp/") || strings.Contains(exeDirLower, "/go-build") {
		wd, err := os.Getwd()
		if err != nil {
			return "."
		}
		return wd
	}
	return exeDir
}

// GetDBFolderPath returns the path to the database folder based on environment variables or platform defaults.
func GetDBFolderPath() string {
	dbFolderPath := os.Getenv("MINIRAY_DB_FOLDER")
	if dbFolderPath != "" {
		return dbFolderPath
	}
	if runtime.GOOS == "windows" {
		return getBaseDir()
	}
	return "/etc/miniray"
}

// GetDBPath returns the full path to the database file.
func GetDBPath() string {
	return fmt.Sprintf("%s/%s.db", GetDBFolderPath(), GetName())
}

// GetUpdateStatusFilePath returns the path to the panel self-update status
// file update.sh writes on completion. It lives beside the database, outside
// MINIRAY_MAIN_FOLDER, so it survives an update regardless of what happens to
// that folder.
func GetUpdateStatusFilePath() string {
	return filepath.Join(GetDBFolderPath(), "update-status.json")
}

// GetDBKind only supports SQLite.
func GetDBKind() string { return "sqlite" }

// GetEnvFilePaths returns the candidate service environment file paths (the file
// systemd loads via EnvironmentFile) across the supported distro families.
func GetEnvFilePaths() []string {
	if runtime.GOOS == "windows" {
		return nil
	}
	return []string{
		"/etc/default/miniray",
		"/etc/conf.d/miniray",
		"/etc/sysconfig/miniray",
	}
}

// GetLogFolder returns the path to the log folder based on environment variables or platform defaults.
func GetLogFolder() string {
	logFolderPath := os.Getenv("MINIRAY_LOG_FOLDER")
	if logFolderPath != "" {
		return logFolderPath
	}
	// Under `go test` the Windows default below is CWD-relative ("./log"), which
	// scatters a log/ directory through the source tree (one per tested package).
	// Redirect test runs to a shared temp folder so the source tree stays clean.
	if testing.Testing() {
		return filepath.Join(os.TempDir(), "miniray-test-log")
	}
	if runtime.GOOS == "windows" {
		return filepath.Join(".", "log")
	}
	return "/var/log/miniray"
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	if err != nil {
		return err
	}

	return out.Sync()
}

func init() {
	if runtime.GOOS != "windows" {
		return
	}
	if os.Getenv("MINIRAY_DB_FOLDER") != "" {
		return
	}
	oldDBFolder := "/etc/miniray"
	oldDBPath := fmt.Sprintf("%s/%s.db", oldDBFolder, GetName())
	newDBFolder := GetDBFolderPath()
	newDBPath := fmt.Sprintf("%s/%s.db", newDBFolder, GetName())
	_, err := os.Stat(newDBPath)
	if err == nil {
		return // new exists
	}
	_, err = os.Stat(oldDBPath)
	if os.IsNotExist(err) {
		return // old does not exist
	}
	_ = copyFile(oldDBPath, newDBPath) // ignore error
}

func GetSecretEncryptionMode() string {
	return strings.TrimSpace(os.Getenv("SECRET_ENCRYPTION"))
}

func GetSecretKeyFile() string {
	if p := strings.TrimSpace(os.Getenv("MINIRAY_SECRET_KEY_FILE")); p != "" {
		return p
	}
	return "/etc/miniray/secret_key.json"
}

func GetSecretKeyEnv() string {
	return "MINIRAY_SECRET_KEY"
}
