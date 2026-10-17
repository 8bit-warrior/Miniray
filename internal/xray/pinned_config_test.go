package xray

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/8bit-warrior/Miniray/internal/util/json_util"
)

func TestPersistedXdnsConfigLoadsInPinnedCore(t *testing.T) {
	binary := os.Getenv("XRAY_E2E_BINARY")
	if binary == "" {
		t.Skip("set XRAY_E2E_BINARY to run the pinned-core configuration check")
	}
	path := filepath.Join(t.TempDir(), "config.json")
	p := NewTestProcess(&Config{
		LogConfig:       json_util.RawMessage(`{"loglevel":"warning"}`),
		InboundConfigs:  []InboundConfig{{Tag: "xdns", Protocol: "dokodemo-door", Port: 24499, Listen: json_util.RawMessage(`"127.0.0.1"`), Settings: json_util.RawMessage(`{"address":"127.0.0.1","port":53,"network":"udp"}`), StreamSettings: json_util.RawMessage(`{"network":"kcp","security":"none","finalmask":{"udp":[{"type":"xdns","settings":{"domains":[{"name":"t.example.test","types":[16],"edns0":1232}]}}]}}`)}},
		OutboundConfigs: json_util.RawMessage(`[{"protocol":"freedom","tag":"direct"}]`),
	}, path)
	if err := p.PersistConfig(); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(binary, "run", "-test", "-c", path).CombinedOutput()
	if err != nil {
		t.Fatalf("pinned core rejected persisted config: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "Configuration OK") {
		t.Fatalf("unexpected pinned-core validation output: %s", out)
	}
}

func TestPersistedXdnsResolverConfigLoadsInPinnedCore(t *testing.T) {
	binary := os.Getenv("XRAY_E2E_BINARY")
	if binary == "" {
		t.Skip("set XRAY_E2E_BINARY to run the pinned-core configuration check")
	}
	path := filepath.Join(t.TempDir(), "config.json")
	p := NewTestProcess(&Config{
		LogConfig:       json_util.RawMessage(`{"loglevel":"warning"}`),
		OutboundConfigs: json_util.RawMessage(`[{"protocol":"vless","tag":"xdns-client","settings":{"vnext":[{"address":"127.0.0.1","port":24499,"users":[{"id":"550e8400-e29b-41d4-a716-446655440000","encryption":"none"}]}]},"streamSettings":{"network":"kcp","security":"none","finalmask":{"udp":[{"type":"xdns","settings":{"domains":[{"name":"t.example.test","types":[1,16],"edns0":1232}],"resolvers":[{"type":"udp","settings":{"addr":"127.0.0.1:53"}}]}}]}}}]`),
	}, path)
	if err := p.PersistConfig(); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(binary, "run", "-test", "-c", path).CombinedOutput()
	if err != nil || !strings.Contains(string(out), "Configuration OK") {
		t.Fatalf("pinned core rejected XDNS resolver: %v\n%s", err, out)
	}
}
