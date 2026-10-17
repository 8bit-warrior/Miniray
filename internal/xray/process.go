package xray

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/8bit-warrior/Miniray/internal/config"
	"github.com/8bit-warrior/Miniray/internal/logger"
	"github.com/8bit-warrior/Miniray/internal/util/common"
	"github.com/8bit-warrior/Miniray/internal/util/maskcompat"
)

// GetBinaryName returns the Xray binary filename for the current OS and architecture.
func GetBinaryName() string {
	arch := runtime.GOARCH
	if arch == "arm" {
		arch = "arm32"
	}
	name := fmt.Sprintf("xray-%s-%s", runtime.GOOS, arch)
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return name
}

// GetBinaryPath returns the full path to the Xray binary executable.
func GetBinaryPath() string {
	return config.GetBinFolderPath() + "/" + GetBinaryName()
}

// GetConfigPath returns the path to the Xray configuration file in the binary folder.
func GetConfigPath() string {
	return config.GetBinFolderPath() + "/config.json"
}

// GetGeositePath returns the path to the geosite data file used by Xray.
func GetGeositePath() string {
	return config.GetBinFolderPath() + "/geosite.dat"
}

// GetGeoipPath returns the path to the geoip data file used by Xray.
func GetGeoipPath() string {
	return config.GetBinFolderPath() + "/geoip.dat"
}

// GetIPLimitLogPath returns the path to the IP limit log file.
func GetIPLimitLogPath() string {
	return config.GetLogFolder() + "/miniray-ipl.log"
}

// GetIPLimitBannedLogPath returns the path to the banned IP log file.
func GetIPLimitBannedLogPath() string {
	return config.GetLogFolder() + "/miniray-ipl-banned.log"
}

// GetIPLimitBannedPrevLogPath returns the path to the previous banned IP log file.
func GetIPLimitBannedPrevLogPath() string {
	return config.GetLogFolder() + "/miniray-ipl-banned.prev.log"
}

func getLogPath(key string) (string, error) {
	config, err := os.ReadFile(GetConfigPath())
	if err != nil {
		logger.Warningf("Failed to read configuration file: %s", err)
		return "", err
	}

	jsonConfig := map[string]any{}
	err = json.Unmarshal(config, &jsonConfig)
	if err != nil {
		logger.Warningf("Failed to parse JSON configuration: %s", err)
		return "", err
	}

	if jsonLog, ok := jsonConfig["log"].(map[string]any); ok {
		if logPath, ok := jsonLog[key].(string); ok {
			return logPath, nil
		}
	}
	return "", nil
}

// GetAccessLogPath reads the Xray config and returns the access log file path.
func GetAccessLogPath() (string, error) {
	return getLogPath("access")
}

// GetErrorLogPath reads the Xray config and returns the error log file path.
func GetErrorLogPath() (string, error) {
	return getLogPath("error")
}

// stopProcess calls Stop on the given Process instance.
func stopProcess(p *Process) {
	_ = p.Stop()
}

// Process wraps an Xray process instance and provides management methods.
type Process struct {
	*process
}

// NewProcess creates a new Xray process and sets up cleanup on garbage collection.
func NewProcess(xrayConfig *Config) *Process {
	p := &Process{newProcess(xrayConfig)}
	runtime.SetFinalizer(p, stopProcess)
	return p
}

// NewTestProcess creates a new Xray process that uses a specific config file path.
// Used for test runs (e.g. outbound test) so the main config.json is not overwritten.
// The config file at configPath is removed when the process is stopped.
func NewTestProcess(xrayConfig *Config, configPath string) *Process {
	p := &Process{newTestProcess(xrayConfig, configPath)}
	runtime.SetFinalizer(p, stopProcess)
	return p
}

type process struct {
	// mu guards the process lifecycle fields (cmd, done, exitErr) plus version,
	// apiPort, and config, which are written by Start/startCommand/refreshVersion/
	// refreshAPIPort/SetConfig
	// while being read concurrently by IsRunning/GetErr/GetResult/GetXrayVersion/
	// GetAPIPort/Stop from other goroutines (status endpoint, check-xray-running
	// and traffic jobs). Snapshot under the lock, then do any blocking syscall
	// (Wait/Signal/Kill) on the local copy without holding it.
	mu          sync.RWMutex
	cmd         *exec.Cmd
	done        chan struct{}
	managedCore bool

	version string
	apiPort int

	onlineClients []string
	// localActiveInbounds is the set of THIS panel's inbound tags that
	// carried traffic within the same grace window. Xray's user>>>email
	// stat aggregates across every inbound a client is attached to, so an
	// online email alone can't say which inbound it actually used. Pairing
	// it with the inbound>>>tag stat lets the per-inbound view drop a
	// multi-inbound client from inbounds that saw no traffic this window.
	localActiveInbounds []string
	localLastOnline     map[string]int64
	// localInboundLastActive mirrors localLastOnline for inbound tags: the
	// last tick this panel's xray reported traffic through each tag.
	// Rebuilt into localActiveInbounds under the same grace window so the
	// two signals stay aligned — an email within grace always has the
	// inbound it used within grace too.
	localInboundLastActive map[string]int64
	onlineMu               sync.RWMutex

	// onlineAPISupport caches whether the running core implements the
	// online-stats RPCs (GetUsersStats). A new process is created on every
	// restart/version switch, so the flag resets to Unknown and is re-probed
	// lazily by the first caller.
	onlineAPISupport atomic.Int32

	config     *Config
	configPath string // if set, use this path instead of GetConfigPath() and remove on Stop
	logWriter  *LogWriter
	exitErr    error
	startTime  time.Time

	intentionalStop atomic.Bool
}

// OnlineAPISupport describes whether the running Xray core implements the
// online-stats API (statsUserOnline + GetUsersStats).
type OnlineAPISupport int32

const (
	// OnlineAPIUnknown means support has not been probed yet for this process.
	OnlineAPIUnknown OnlineAPISupport = iota
	// OnlineAPISupported means the core answered the online-stats RPC.
	OnlineAPISupported
	// OnlineAPIUnsupported means the core returned Unimplemented (older binary).
	OnlineAPIUnsupported
)

// OnlineAPISupport returns the cached online-stats capability of this process.
func (p *process) OnlineAPISupport() OnlineAPISupport {
	return OnlineAPISupport(p.onlineAPISupport.Load())
}

// SetOnlineAPISupport records the probed online-stats capability of this process.
func (p *process) SetOnlineAPISupport(v OnlineAPISupport) {
	p.onlineAPISupport.Store(int32(v))
}

var (
	xrayGracefulStopTimeout = 5 * time.Second
	xrayForceStopTimeout    = 2 * time.Second
	xrayVersionTimeout      = 5 * time.Second
	// OnCrash is called when xray crashes unexpectedly. Set from web layer.
	OnCrash func(err error)
)

// newProcess creates a new internal process struct for Xray.
func newProcess(config *Config) *process {
	return &process{
		version:   "Unknown",
		config:    config,
		logWriter: NewLogWriter(),
		startTime: time.Now(),
	}
}

// newTestProcess creates a process that writes and runs with a specific config path.
func newTestProcess(config *Config, configPath string) *process {
	p := newProcess(config)
	p.configPath = configPath
	return p
}

// IsRunning returns true if the Xray process is currently running.
func (p *process) IsRunning() bool {
	p.mu.RLock()
	cmd, done := p.cmd, p.done
	p.mu.RUnlock()
	if cmd == nil || cmd.Process == nil {
		return false
	}
	// done is closed by the waitForCommand goroutine exactly when cmd.Wait
	// returns, i.e. when the process has exited; it is the race-free signal here
	// (reading cmd.ProcessState would race with that Wait).
	if done != nil {
		select {
		case <-done:
			return false
		default:
		}
	}
	return true
}

// GetErr returns the last error encountered by the Xray process.
func (p *process) GetErr() error {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.exitErr
}

// GetResult returns the last log line or error from the Xray process.
func (p *process) GetResult() string {
	p.mu.RLock()
	exitErr := p.exitErr
	p.mu.RUnlock()
	lastLine := p.logWriter.LastLine()
	if len(lastLine) == 0 && exitErr != nil {
		return exitErr.Error()
	}
	return lastLine
}

// GetXrayVersion returns the version string of the Xray process.
func (p *process) GetXrayVersion() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.version
}

// GetAPIPort returns the API port used by the Xray process.
func (p *Process) GetAPIPort() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.apiPort
}

// GetConfig returns the configuration used by the Xray process.
func (p *Process) GetConfig() *Config {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.config
}

// SetConfig replaces the stored configuration snapshot after the running
// process has been reconciled with it through the gRPC API (hot apply), so
// later change detection compares against what is actually running.
func (p *Process) SetConfig(config *Config) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.config = config
}

// PersistConfig writes the current configuration snapshot to the config file,
// keeping it in step after a hot apply (Start only writes it on a cold start).
func (p *Process) PersistConfig() error {
	p.mu.RLock()
	data, err := marshalPinnedConfig(p.config)
	path := p.configPath
	p.mu.RUnlock()
	if err != nil {
		return common.NewErrorf("Failed to generate XRAY configuration files: %v", err)
	}
	if path == "" {
		path = GetConfigPath()
	}
	return writeFileAtomic(path, data, 0o600)
}

func (p *Process) GetOnlineClients() []string { return p.GetLocalOnlineClients() }

func (p *Process) GetLocalOnlineClients() []string {
	p.onlineMu.RLock()
	defer p.onlineMu.RUnlock()
	if len(p.onlineClients) == 0 {
		return nil
	}
	out := make([]string, len(p.onlineClients))
	copy(out, p.onlineClients)
	return out
}

func (p *Process) GetLocalActiveInbounds() []string {
	p.onlineMu.RLock()
	defer p.onlineMu.RUnlock()
	if len(p.localActiveInbounds) == 0 {
		return nil
	}
	out := make([]string, len(p.localActiveInbounds))
	copy(out, p.localActiveInbounds)
	return out
}

func (p *Process) RefreshLocalOnline(activeEmails, activeInboundTags []string, now, graceMs int64) {
	p.onlineMu.Lock()
	defer p.onlineMu.Unlock()
	if p.localLastOnline == nil {
		p.localLastOnline = make(map[string]int64, len(activeEmails))
	}
	for _, email := range activeEmails {
		p.localLastOnline[email] = now
	}
	online := make([]string, 0, len(p.localLastOnline))
	for email, ts := range p.localLastOnline {
		if now-ts < graceMs {
			online = append(online, email)
		} else {
			delete(p.localLastOnline, email)
		}
	}
	p.onlineClients = online

	if p.localInboundLastActive == nil {
		p.localInboundLastActive = make(map[string]int64, len(activeInboundTags))
	}
	for _, tag := range activeInboundTags {
		p.localInboundLastActive[tag] = now
	}
	activeInbounds := make([]string, 0, len(p.localInboundLastActive))
	for tag, ts := range p.localInboundLastActive {
		if now-ts < graceMs {
			activeInbounds = append(activeInbounds, tag)
		} else {
			delete(p.localInboundLastActive, tag)
		}
	}
	p.localActiveInbounds = activeInbounds
}

// GetUptime returns the uptime of the Xray process in seconds.
func (p *Process) GetUptime() uint64 {
	return uint64(time.Since(p.startTime).Seconds())
}

// refreshAPIPort updates the API port from the inbound configs.
func (p *process) refreshAPIPort() {
	port := 0
	for _, inbound := range p.config.InboundConfigs {
		if inbound.Tag == "api" {
			port = inbound.Port
			break
		}
	}
	p.mu.Lock()
	p.apiPort = port
	p.mu.Unlock()
}

var installedVersionCache struct {
	sync.Mutex
	path     string
	size     int64
	modified time.Time
	value    string
}

func InstalledVersion() string {
	path := GetBinaryPath()
	info, err := os.Stat(path)
	if err != nil {
		return "Unknown"
	}
	installedVersionCache.Lock()
	defer installedVersionCache.Unlock()
	if installedVersionCache.path == path && installedVersionCache.size == info.Size() &&
		installedVersionCache.modified.Equal(info.ModTime()) {
		return installedVersionCache.value
	}
	version := "Unknown"
	ctx, cancel := context.WithTimeout(context.Background(), xrayVersionTimeout)
	defer cancel()
	if data, err := exec.CommandContext(ctx, path, "-version").Output(); err == nil {
		if fields := bytes.Fields(data); len(fields) > 1 && string(fields[0]) == "Xray" {
			version = string(fields[1])
		}
	}
	installedVersionCache.path = path
	installedVersionCache.size = info.Size()
	installedVersionCache.modified = info.ModTime()
	installedVersionCache.value = version
	return version
}

func (p *process) refreshVersion() {
	version := InstalledVersion()
	p.mu.Lock()
	p.version = version
	p.mu.Unlock()
}

// Start launches the Xray process with the current configuration.
func (p *process) Start() error {
	return p.start(false)
}

func (p *process) StartWithRestart(forceRestart bool) error {
	return p.start(forceRestart)
}

func (p *process) start(forceRestart bool) (err error) {
	if p.IsRunning() {
		return errors.New("xray is already running")
	}

	defer func() {
		if err != nil {
			logger.Error("Failure in running xray-core process: ", err)
			p.setExitErr(err)
		}
	}()

	data, err := marshalPinnedConfig(p.config)
	if err != nil {
		return common.NewErrorf("Failed to generate XRAY configuration files: %v", err)
	}

	err = os.MkdirAll(config.GetLogFolder(), 0o770)
	if err != nil {
		logger.Warningf("Failed to create log folder: %s", err)
	}

	configPath := GetConfigPath()
	if p.configPath != "" {
		configPath = p.configPath
	}
	if p.configPath == "" && independentCoreEnabled() {
		existing, _ := os.ReadFile(configPath)
		return p.startIndependentCore(configPath, existing, data, forceRestart)
	}
	err = writeFileAtomic(configPath, data, 0o600)
	if err != nil {
		return common.NewErrorf("Failed to write configuration file: %v", err)
	}

	cmd := exec.CommandContext(context.Background(), GetBinaryPath(), "-c", configPath)
	cmd.Stdout = p.logWriter
	cmd.Stderr = p.logWriter

	err = p.startCommand(cmd)
	if err != nil {
		return err
	}

	p.refreshVersion()
	p.refreshAPIPort()

	return nil
}

// writeFileAtomic writes data to path via a same-directory temp file that is
// permissioned, synced, and renamed into place, so a crash can never leave a
// partial config; the config holds credentials, hence the 0600 perm. After the
// rename the parent directory is fsynced to persist the directory entry. That
// final step is skipped on Windows, where directory fsync is unsupported and
// os.Rename already uses replace-existing semantics.
func writeFileAtomic(path string, data []byte, perm os.FileMode) (err error) {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".config-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() {
		_ = tmp.Close()
		if err != nil {
			_ = os.Remove(tmpPath)
		}
	}()
	if err = tmp.Chmod(perm); err != nil {
		return err
	}
	if _, err = tmp.Write(data); err != nil {
		return err
	}
	if err = tmp.Sync(); err != nil {
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	if err = renameFile(tmpPath, path); err != nil {
		return err
	}
	if runtime.GOOS == "windows" {
		return nil
	}
	dirHandle, err := os.Open(dir)
	if err != nil {
		return err
	}
	err = dirHandle.Sync()
	_ = dirHandle.Close()
	return err
}

var renameFile = os.Rename

func (p *process) startCommand(cmd *exec.Cmd) error {
	p.mu.Lock()
	p.cmd = cmd
	p.done = make(chan struct{})
	p.exitErr = nil
	done := p.done
	p.mu.Unlock()
	p.intentionalStop.Store(false)

	if err := cmd.Start(); err != nil {
		close(done)
		p.mu.Lock()
		p.cmd = nil
		p.mu.Unlock()
		return err
	}

	attachChildLifetime(cmd)

	go p.waitForCommand(cmd, done)
	return nil
}

func (p *process) setExitErr(err error) {
	p.mu.Lock()
	p.exitErr = err
	p.mu.Unlock()
}

func (p *process) waitForCommand(cmd *exec.Cmd, done chan struct{}) {
	defer close(done)

	err := cmd.Wait()
	if err == nil || p.intentionalStop.Load() {
		return
	}

	// On Windows, killing the process results in "exit status 1" which isn't an error for us.
	if runtime.GOOS == "windows" {
		errStr := strings.ToLower(err.Error())
		if strings.Contains(errStr, "exit status 1") {
			p.setExitErr(err)
			return
		}
	}

	logger.Error("Failure in running xray-core:", err)
	p.setExitErr(err)
	if OnCrash != nil {
		OnCrash(err)
	}
}

// Stop terminates the running Xray process.
func (p *process) Stop() error {
	if !p.IsRunning() {
		return errors.New("xray is not running")
	}
	p.intentionalStop.Store(true)

	if p.managedCore {
		if err := stopIndependentCore(); err != nil {
			return err
		}
		return p.waitForExit(xrayForceStopTimeout + xrayGracefulStopTimeout)
	}
	// Snapshot cmd once, then run the blocking Signal/Kill/Wait on the local copy
	// without holding the lock.
	p.mu.RLock()
	cmd := p.cmd
	p.mu.RUnlock()
	if cmd == nil || cmd.Process == nil {
		return errors.New("xray is not running")
	}

	// Remove temporary config file used for test runs so main config is never touched
	if p.configPath != "" {
		if p.configPath != GetConfigPath() {
			// Check if file exists before removing
			if _, err := os.Stat(p.configPath); err == nil {
				_ = os.Remove(p.configPath)
			}
		}
	}

	if runtime.GOOS == "windows" {
		if err := cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			return err
		}
		return p.waitForExit(xrayForceStopTimeout)
	}

	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		if errors.Is(err, os.ErrProcessDone) {
			return p.waitForExit(xrayForceStopTimeout)
		}
		return err
	}

	if err := p.waitForExit(xrayGracefulStopTimeout); err == nil {
		return nil
	}

	logger.Warning("xray-core did not stop after SIGTERM, killing process")
	if err := cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	return p.waitForExit(xrayForceStopTimeout)
}

func (p *process) waitForExit(timeout time.Duration) error {
	p.mu.RLock()
	done := p.done
	p.mu.RUnlock()
	if done == nil {
		return nil
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case <-done:
		return nil
	case <-timer.C:
		return common.NewErrorf("timed out waiting for xray-core process to stop after %s", timeout)
	}
}

const (
	crashReportPrefix = "core_crash_"
	crashReportSuffix = ".log"
	maxCrashReports   = 10
)

// writeCrashReport persists a captured xray crash chunk to the log folder
// with nanosecond-precision filename so restart-loop bursts don't overwrite
// each other, and prunes old reports to keep the folder bounded.
func writeCrashReport(m []byte) error {
	dir := config.GetLogFolder()
	if err := os.MkdirAll(dir, 0o770); err != nil {
		return err
	}
	pruneOldCrashReports(dir, maxCrashReports-1)
	name := crashReportPrefix + time.Now().Format("20060102_150405_000000000") + crashReportSuffix
	return os.WriteFile(filepath.Join(dir, name), m, 0o640)
}

func pruneOldCrashReports(dir string, keep int) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	var reports []string
	for _, e := range entries {
		n := e.Name()
		if !e.IsDir() && strings.HasPrefix(n, crashReportPrefix) && strings.HasSuffix(n, crashReportSuffix) {
			reports = append(reports, n)
		}
	}
	if len(reports) <= keep {
		return
	}
	sort.Strings(reports)
	for _, old := range reports[:len(reports)-keep] {
		_ = os.Remove(filepath.Join(dir, old))
	}
}

func marshalPinnedConfig(config *Config) ([]byte, error) {
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return nil, err
	}
	return maskcompat.PinnedConfigJSON(data)
}
