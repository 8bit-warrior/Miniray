// Package service provides business logic services for the miniray web panel,
// including inbound/outbound management, user administration, settings, and Xray integration.
package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/8bit-warrior/Miniray/internal/amneziawg"
	"github.com/8bit-warrior/Miniray/internal/database"
	"github.com/8bit-warrior/Miniray/internal/database/model"
	"github.com/8bit-warrior/Miniray/internal/logger"
	"github.com/8bit-warrior/Miniray/internal/tuic"
	"github.com/8bit-warrior/Miniray/internal/util/common"
	"github.com/8bit-warrior/Miniray/internal/util/maskcompat"
	"github.com/8bit-warrior/Miniray/internal/util/netsafe"
	wgutil "github.com/8bit-warrior/Miniray/internal/util/wireguard"
	"github.com/8bit-warrior/Miniray/internal/xray"
)

type InboundService struct {
	clientService   ClientService
	fallbackService FallbackService
}

func normalizeTrafficResetDay(day int) int {
	if day < 1 {
		return 1
	}
	return min(day, 31)
}

func normalizeInboundShareAddrStrategy(strategy string) string {
	strategy = strings.TrimSpace(strategy)
	switch strategy {
	case "listen", "custom":
		return strategy
	default:
		return "listen"
	}
}

func normalizeInboundShareAddress(inbound *model.Inbound) {
	if inbound == nil {
		return
	}
	
	inbound.ShareAddrStrategy = normalizeInboundShareAddrStrategy(inbound.ShareAddrStrategy)
	if addr, err := normalizeInboundShareHost(inbound.ShareAddr); err == nil {
		inbound.ShareAddr = addr
	} else {
		inbound.ShareAddr = strings.TrimSpace(inbound.ShareAddr)
	}
}

func normalizeInboundShareAddressStrict(inbound *model.Inbound) error {
	if inbound == nil {
		return nil
	}
	
	inbound.ShareAddrStrategy = normalizeInboundShareAddrStrategy(inbound.ShareAddrStrategy)
	addr, err := normalizeInboundShareHost(inbound.ShareAddr)
	if err != nil {
		return common.NewError("shareAddr must be a host or IP without scheme or port")
	}
	inbound.ShareAddr = addr
	return nil
}

func normalizeInboundShareHost(raw string) (string, error) {
	addr := strings.TrimSpace(raw)
	if addr == "" {
		return "", nil
	}
	if strings.Contains(addr, "://") || strings.HasPrefix(addr, "//") || strings.ContainsAny(addr, "/?#@") {
		return "", fmt.Errorf("invalid share address %q", raw)
	}
	if strings.HasPrefix(addr, "[") {
		if !strings.HasSuffix(addr, "]") {
			return "", fmt.Errorf("invalid IPv6 host %q", raw)
		}
		ip := net.ParseIP(addr[1 : len(addr)-1])
		if ip == nil || ip.To4() != nil {
			return "", fmt.Errorf("invalid IPv6 host %q", raw)
		}
		return "[" + ip.String() + "]", nil
	}
	if strings.Contains(addr, ":") {
		if _, _, err := net.SplitHostPort(addr); err == nil {
			return "", fmt.Errorf("share address must not include port")
		}
		ip := net.ParseIP(addr)
		if ip == nil || ip.To4() != nil {
			return "", fmt.Errorf("invalid IPv6 host %q", raw)
		}
		return "[" + ip.String() + "]", nil
	}
	host, err := netsafe.NormalizeHost(addr)
	if err != nil {
		return "", err
	}
	return host, nil
}

func normalizeInboundShareAddressColumns(tx *gorm.DB) error {
	if tx == nil || !tx.Migrator().HasColumn(&model.Inbound{}, "share_addr_strategy") {
		return nil
	}

	strategyExpr := `CASE TRIM(COALESCE(share_addr_strategy, '')) WHEN 'listen' THEN 'listen' WHEN 'custom' THEN 'custom' ELSE 'listen' END`
	if err := tx.Exec(`UPDATE inbounds SET share_addr_strategy = ` + strategyExpr + ` WHERE share_addr_strategy IS NULL OR share_addr_strategy <> ` + strategyExpr).Error; err != nil {
		return err
	}
	hasShareAddr := tx.Migrator().HasColumn(&model.Inbound{}, "share_addr")
	if hasShareAddr {
		if err := tx.Exec(`UPDATE inbounds SET share_addr = TRIM(share_addr) WHERE share_addr IS NOT NULL AND share_addr <> TRIM(share_addr)`).Error; err != nil {
			return err
		}
	}
	if !hasShareAddr {
		return nil
	}
	var rows []struct {
		Id                int
		ShareAddrStrategy string
		ShareAddr         string
	}
	if err := tx.Model(&model.Inbound{}).Select("id", "share_addr_strategy", "share_addr").Find(&rows).Error; err != nil {
		return err
	}
	for _, row := range rows {
		strategy := normalizeInboundShareAddrStrategy(row.ShareAddrStrategy)
		addr, addrErr := normalizeInboundShareHost(row.ShareAddr)
		if addrErr != nil {
			strategy = "listen"
			addr = ""
		}
		updates := map[string]any{}
		if strategy != row.ShareAddrStrategy {
			updates["share_addr_strategy"] = strategy
		}
		if addr != row.ShareAddr {
			updates["share_addr"] = addr
		}
		if len(updates) > 0 {
			if err := tx.Model(&model.Inbound{}).Where("id = ?", row.Id).Updates(updates).Error; err != nil {
				return err
			}
		}
	}
	return nil
}

// GetInbounds retrieves all inbounds for a specific user with client stats.
func (s *InboundService) GetInbounds(userId int) ([]*model.Inbound, error) {
	db := database.GetDB()
	var inbounds []*model.Inbound
	err := db.Model(model.Inbound{}).Preload("ClientStats").Where("user_id = ?", userId).Order("id ASC").Find(&inbounds).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	s.enrichClientStats(db, inbounds)
	s.annotateFallbackParents(db, inbounds)

	return inbounds, nil
}

// GetInboundsSlim returns the same list of inbounds as GetInbounds but
// strips every per-client field other than email / enable / comment from
// settings.clients and skips UUID/SubId enrichment on ClientStats. The
// inbounds page only needs those three to roll up client counts and
// render badges, so this trims tens of bytes per client (UUID, password,
// flow, security, totalGB, expiryTime, limitIp, tgId, ...) which adds
// up fast on installs with thousands of clients.
//
// Full client data is still available through GET /panel/api/inbounds/get/:id
// for the edit/info/qr/export/clone flows that need it.
func (s *InboundService) GetInboundsSlim(userId int) ([]*model.Inbound, error) {
	db := database.GetDB()
	var inbounds []*model.Inbound
	err := db.Model(model.Inbound{}).Preload("ClientStats").Where("user_id = ?", userId).Order("id ASC").Find(&inbounds).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	s.annotateFallbackParents(db, inbounds)

	// Top up stats rows owned by sibling inbounds (multi-attached clients)
	// so the list's depleted/expiring badges see every client; the UUID/SubId
	// enrichment stays skipped. Must run before slimming strips the settings.
	s.backfillClientStats(db, inbounds)

	for _, ib := range inbounds {
		ib.Settings = slimSettingsClients(ib.Settings)
	}
	return inbounds, nil
}

// slimSettingsClients rewrites the inbound settings JSON so settings.clients[]
// keeps only the fields the list view actually reads. Returns the input
// unchanged when the JSON can't be parsed or has no clients array.
func slimSettingsClients(settings string) string {
	if settings == "" {
		return settings
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(settings), &raw); err != nil {
		return settings
	}
	clients, ok := raw["clients"].([]any)
	if !ok || len(clients) == 0 {
		return settings
	}
	slim := make([]any, 0, len(clients))
	for _, entry := range clients {
		c, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		row := make(map[string]any, 3)
		if v, ok := c["email"]; ok {
			row["email"] = v
		}
		if v, ok := c["enable"]; ok {
			row["enable"] = v
		}
		if v, ok := c["comment"]; ok && v != "" {
			row["comment"] = v
		}
		slim = append(slim, row)
	}
	raw["clients"] = slim
	out, err := json.Marshal(raw)
	if err != nil {
		return settings
	}
	return string(out)
}

func (s *InboundService) annotateFallbackParents(db *gorm.DB, inbounds []*model.Inbound) {
	if len(inbounds) == 0 {
		return
	}
	childIds := make([]int, 0, len(inbounds))
	for _, ib := range inbounds {
		childIds = append(childIds, ib.Id)
	}
	var rows []model.InboundFallback
	if err := db.Where("child_id IN ?", childIds).
		Order("sort_order ASC, id ASC").
		Find(&rows).Error; err != nil {
		return
	}
	first := make(map[int]model.InboundFallback, len(rows))
	for _, r := range rows {
		if _, ok := first[r.ChildId]; !ok {
			first[r.ChildId] = r
		}
	}
	for _, ib := range inbounds {
		if r, ok := first[ib.Id]; ok {
			ib.FallbackParent = &model.FallbackParentInfo{
				MasterId: r.MasterId,
				Path:     r.Path,
			}
		}
	}
}

type InboundOption struct {
	Id             int    `json:"id" example:"1"`
	Remark         string `json:"remark" example:"VLESS-443"`
	Tag            string `json:"tag" example:"in-443-tcp"`
	Protocol       string `json:"protocol" example:"vless"`
	Port           int    `json:"port" example:"443"`
	Enable         bool   `json:"enable" example:"true"`
	Network        string `json:"network,omitempty"`
	Security       string `json:"security,omitempty"`
	TlsFlowCapable bool   `json:"tlsFlowCapable" example:"true"`
	SsMethod       string `json:"ssMethod"`
	WgPublicKey    string `json:"wgPublicKey,omitempty"`
	WgMtu          int    `json:"wgMtu,omitempty"`
	WgDns          string `json:"wgDns,omitempty"`
	// AwgServer carries the full AmneziaWG server block (keys, subnet,
	// obfuscation params) so the clients page can render a downloadable
	// per-client .conf without a second round trip.
	AwgServer         *amneziawg.ServerSettings `json:"awgServer,omitempty"`
	TuicServer        *tuic.TuicServerSettings  `json:"tuicServer,omitempty"`
	Listen            string                    `json:"listen,omitempty"`
	ShareAddr         string                    `json:"shareAddr,omitempty"`
	ShareAddrStrategy string                    `json:"shareAddrStrategy,omitempty"`
}

func (s *InboundService) GetInboundOptions(userId int) ([]InboundOption, error) {
	db := database.GetDB()
	var rows []struct {
		Id                int    `gorm:"column:id"`
		Remark            string `gorm:"column:remark"`
		Tag               string `gorm:"column:tag"`
		Protocol          string `gorm:"column:protocol"`
		Port              int    `gorm:"column:port"`
		Enable            bool   `gorm:"column:enable"`
		StreamSettings    string `gorm:"column:stream_settings"`
		Settings          string `gorm:"column:settings"`
		Listen            string `gorm:"column:listen"`
		ShareAddr         string `gorm:"column:share_addr"`
		ShareAddrStrategy string `gorm:"column:share_addr_strategy"`
		DisableFlow       bool   `gorm:"column:disable_flow"`
	}
	err := db.Table("inbounds").
		Select("inbounds.id, inbounds.remark, inbounds.tag, inbounds.protocol, inbounds.port, inbounds.enable, inbounds.stream_settings, inbounds.settings, inbounds.listen, inbounds.share_addr, inbounds.share_addr_strategy, inbounds.disable_flow").
		Where("inbounds.user_id = ?", userId).
		Order("inbounds.id ASC").
		Scan(&rows).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	out := make([]InboundOption, 0, len(rows))
	for _, r := range rows {
		wgPublicKey, wgMtu, wgDns := inboundWireguardHints(r.Protocol, r.Settings)
		netHint, secHint := inboundStreamHints(r.Protocol, r.StreamSettings, r.Settings)
		shareAddrStrategy := r.ShareAddrStrategy
		if shareAddrStrategy == "listen" {
			shareAddrStrategy = ""
		}
		out = append(out, InboundOption{
			Id:                r.Id,
			Remark:            r.Remark,
			Tag:               r.Tag,
			Protocol:          r.Protocol,
			Port:              r.Port,
			Enable:            r.Enable,
			Network:           netHint,
			Security:          secHint,
			TlsFlowCapable:    !r.DisableFlow && inboundCanEnableTlsFlow(r.Protocol, r.StreamSettings, r.Settings),
			SsMethod:          inboundShadowsocksMethod(r.Protocol, r.Settings),
			WgPublicKey:       wgPublicKey,
			WgMtu:             wgMtu,
			WgDns:             wgDns,
			AwgServer:         inboundAmneziaWGServer(r.Protocol, r.Settings),
			TuicServer:        inboundTuicServer(r.Protocol, r.Settings),
			Listen:            r.Listen,
			ShareAddr:         r.ShareAddr,
			ShareAddrStrategy: shareAddrStrategy,
		})
	}
	return out, nil
}

func inboundStreamHints(protocol string, streamSettings string, settings string) (string, string) {
	p := strings.ToLower(protocol)
	if p == "wireguard" || p == "amneziawg" || p == "hysteria" {
		return "udp", ""
	}
	var netHint, secHint string
	if strings.TrimSpace(streamSettings) != "" {
		var raw struct {
			Network  string `json:"network"`
			Security string `json:"security"`
		}
		if err := json.Unmarshal([]byte(streamSettings), &raw); err == nil {
			netHint = raw.Network
			secHint = raw.Security
		}
	}
	if netHint == "" && strings.TrimSpace(settings) != "" {
		var raw struct {
			Network        string `json:"network"`
			AllowedNetwork string `json:"allowedNetwork"`
			UDP            bool   `json:"udp"`
		}
		if err := json.Unmarshal([]byte(settings), &raw); err == nil {
			if raw.Network != "" {
				netHint = raw.Network
			} else if raw.AllowedNetwork != "" {
				netHint = raw.AllowedNetwork
			} else if raw.UDP {
				netHint = "tcp,udp"
			}
		}
	}
	if netHint == "" {
		netHint = "tcp"
	}
	return netHint, secHint
}

func inboundWireguardHints(protocol string, settings string) (string, int, string) {
	if protocol != string(model.WireGuard) || strings.TrimSpace(settings) == "" {
		return "", 0, ""
	}
	var parsed struct {
		PublicKey string `json:"publicKey"`
		PubKey    string `json:"pubKey"`
		SecretKey string `json:"secretKey"`
		MTU       int    `json:"mtu"`
		DNS       string `json:"dns"`
	}
	if err := json.Unmarshal([]byte(settings), &parsed); err != nil {
		return "", 0, ""
	}
	publicKey := parsed.PublicKey
	if publicKey == "" {
		publicKey = parsed.PubKey
	}
	if publicKey == "" && parsed.SecretKey != "" {
		if derived, err := wgutil.PublicKeyFromPrivate(parsed.SecretKey); err == nil {
			publicKey = derived
		}
	}
	return publicKey, parsed.MTU, parsed.DNS
}

// inboundAmneziaWGServer returns the AmneziaWG server block for the clients
// page's config-download builder, or nil when the inbound isn't AmneziaWG or
// its settings don't parse. PrivateKey is redacted: GetInboundOptions is a
// shared, admin-wide list used to fill dropdowns, not a place a live tunnel
// secret needs to travel — the frontend's own AwgServerOptionSchema never
// reads it, so nothing is lost by not sending it, and it shouldn't widen the
// blast radius of a log capture, proxy cache, or browser devtools screenshot.
func inboundAmneziaWGServer(protocol string, settings string) *amneziawg.ServerSettings {
	if protocol != string(model.AmneziaWG) || strings.TrimSpace(settings) == "" {
		return nil
	}
	var parsed amneziawg.InboundSettings
	if err := json.Unmarshal([]byte(settings), &parsed); err != nil || parsed.Server == nil {
		return nil
	}
	redacted := *parsed.Server
	redacted.PrivateKey = ""
	return &redacted
}

func inboundTuicServer(protocol string, settings string) *tuic.TuicServerSettings {
	if protocol != string(model.TUIC) || strings.TrimSpace(settings) == "" {
		return nil
	}
	inst, ok := tuic.InstanceFromInbound(&model.Inbound{Protocol: model.TUIC, Settings: settings})
	if !ok {
		return nil
	}
	return &tuic.TuicServerSettings{
		Certificate:           inst.Certificate,
		CongestionControl:     inst.CongestionControl,
		ALPN:                  inst.ALPN,
		UDPRelayMode:          inst.UDPRelayMode,
		ZeroRTTHandshake:      inst.ZeroRTTHandshake,
		LogLevel:              inst.LogLevel,
		MaxIdleTime:           inst.MaxIdleTime,
		AuthenticationTimeout: inst.AuthenticationTimeout,
		MaxUdpRelayPacketSize: inst.MaxUdpRelayPacketSize,
		SNI:                   inst.SNI,
	}
}



// GetAllInbounds retrieves all inbounds with client stats.
func (s *InboundService) GetAllInbounds() ([]*model.Inbound, error) {
	db := database.GetDB()
	var inbounds []*model.Inbound
	err := db.Model(model.Inbound{}).Preload("ClientStats").Find(&inbounds).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	s.enrichClientStats(db, inbounds)
	return inbounds, nil
}

func (s *InboundService) GetInboundsByTrafficReset(period string) ([]*model.Inbound, error) {
	db := database.GetDB()
	var inbounds []*model.Inbound
	err := db.Model(model.Inbound{}).Where("traffic_reset = ?", period).Find(&inbounds).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	return inbounds, nil
}

func (s *InboundService) GetClients(inbound *model.Inbound) ([]model.Client, error) {
	return ParseInboundSettingsClients(inbound.Settings)
}

// GetClientsBySubId returns the inbound's clients with the given subscription
// id, resolved from the normalized clients tables (the same source the running
// Xray users are built from) instead of parsing the settings JSON blob.
func (s *InboundService) GetClientsBySubId(inboundId int, subId string) ([]model.Client, error) {
	return s.clientService.ListForInboundBySubId(nil, inboundId, subId)
}

// ListClientsForInbound returns every client attached to the inbound from the
// normalized clients tables — the same source the running Xray config uses —
// instead of parsing the embedded settings JSON, which can hold a stale UUID
// after a client identity change (#6436).
func (s *InboundService) ListClientsForInbound(inboundId int) ([]model.Client, error) {
	return s.clientService.ListForInbound(nil, inboundId)
}

func (s *InboundService) GetAllEmails() ([]string, error) {
	db := database.GetDB()
	var emails []string
	query := fmt.Sprintf(
		"SELECT DISTINCT %s %s",
		database.JSONFieldText("client.value", "email"),
		database.JSONClientsFromInbound(),
	)
	if err := db.Raw(query).Scan(&emails).Error; err != nil {
		return nil, err
	}
	return emails, nil
}

// emailSubIDsForClients returns lower(email)→subId for just the emails being
// checked. One clients row owns an email's identity, so the answer no longer
// needs a scan of every inbound's settings JSON (#6252).
func (s *InboundService) emailSubIDsForClients(clients []model.Client) (map[string]string, error) {
	want := make(map[string]struct{}, len(clients))
	for i := range clients {
		if email := strings.ToLower(strings.TrimSpace(clients[i].Email)); email != "" {
			want[email] = struct{}{}
		}
	}
	result := make(map[string]string, len(want))
	if len(want) == 0 {
		return result, nil
	}
	lowered := make([]string, 0, len(want))
	for email := range want {
		lowered = append(lowered, email)
	}
	db := database.GetDB()
	for _, batch := range chunkStrings(lowered, sqlInChunk) {
		var rows []struct {
			Email string
			SubID string `gorm:"column:sub_id"`
		}
		err := db.Model(&model.ClientRecord{}).
			Select("email, sub_id").
			Where("LOWER(email) IN ?", batch).
			Scan(&rows).Error
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			result[strings.ToLower(r.Email)] = r.SubID
		}
	}
	return result, nil
}

// normalizeStreamSettings clears StreamSettings for protocols that don't use it.
// Only vmess, vless, trojan, shadowsocks, hysteria, wireguard, and tunnel
// protocols use streamSettings (wireguard for finalmask UDP masks and sockopt on
// its listener; tunnel for sockopt, notably sockopt.tproxy for its TProxy/redirect
// mode). Streams keyed on "method" — xray-core v26.7.11's preferred alias for
// "network" — are canonicalized to "network", which every panel reader (link
// generation, port-conflict detection, flow eligibility) keys on.
func (s *InboundService) normalizeStreamSettings(inbound *model.Inbound) {
	protocolsWithStream := map[model.Protocol]bool{
		model.VMESS:       true,
		model.VLESS:       true,
		model.Trojan:      true,
		model.Shadowsocks: true,
		model.Hysteria:    true,
		model.WireGuard:   true,
		model.Tunnel:      true,
	}

	if !protocolsWithStream[inbound.Protocol] {
		inbound.StreamSettings = ""
		return
	}
	inbound.StreamSettings = canonicalizeStreamNetworkKey(inbound.StreamSettings)
	inbound.StreamSettings = canonicalizeLegacyXdnsMasks(inbound.StreamSettings)
}

// canonicalizeLegacyXdnsMasks stores an xdns mask posted in the pre-26.9.30 string
// lists in the object shape the core parses, as GetXrayConfig would heal it anyway.
func canonicalizeLegacyXdnsMasks(streamSettings string) string {
	if streamSettings == "" {
		return streamSettings
	}
	var stream map[string]any
	if err := json.Unmarshal([]byte(streamSettings), &stream); err != nil {
		return streamSettings
	}
	if !maskcompat.UpgradeLegacyXdns(stream["finalmask"]) {
		return streamSettings
	}
	out, err := json.MarshalIndent(stream, "", "  ")
	if err != nil {
		return streamSettings
	}
	return string(out)
}

// canonicalizeStreamNetworkKey rewrites a streamSettings JSON that names its
// transport under "method" to the panel-canonical "network" key. When both
// keys are present, "method" wins — matching xray-core's own precedence.
func canonicalizeStreamNetworkKey(streamSettings string) string {
	if streamSettings == "" {
		return streamSettings
	}
	var stream map[string]any
	if err := json.Unmarshal([]byte(streamSettings), &stream); err != nil {
		return streamSettings
	}
	method, ok := stream["method"].(string)
	if !ok || method == "" {
		return streamSettings
	}
	stream["network"] = method
	delete(stream, "method")
	out, err := json.MarshalIndent(stream, "", "  ")
	if err != nil {
		return streamSettings
	}
	return string(out)
}

func validateInboundTLSCertificates(streamSettings string) error {
	if strings.TrimSpace(streamSettings) == "" {
		return nil
	}
	var stream struct {
		Security    string          `json:"security"`
		TLSSettings json.RawMessage `json:"tlsSettings"`
	}
	if err := json.Unmarshal([]byte(streamSettings), &stream); err != nil {
		return common.NewError("Invalid inbound stream settings: ", err)
	}
	if !strings.EqualFold(stream.Security, "tls") {
		return nil
	}
	var settings struct {
		Certificates []struct {
			CertificateFile string   `json:"certificateFile"`
			KeyFile         string   `json:"keyFile"`
			Certificate     []string `json:"certificate"`
			Key             []string `json:"key"`
			Usage           string   `json:"usage"`
		} `json:"certificates"`
	}
	if len(stream.TLSSettings) > 0 {
		if err := json.Unmarshal(stream.TLSSettings, &settings); err != nil {
			return common.NewError("Invalid inbound TLS settings: ", err)
		}
	}
	hasServerCertificate := false
	for i, cert := range settings.Certificates {
		// Match Xray's file-over-inline precedence for each credential.
		certificate := cert.CertificateFile
		if certificate == "" {
			certificate = strings.Join(cert.Certificate, "\n")
		}
		if strings.TrimSpace(certificate) == "" {
			return common.NewErrorf("TLS certificate %d is missing. Configure a certificate file path or certificate content before saving the inbound.", i+1)
		}
		if strings.EqualFold(cert.Usage, "verify") {
			continue
		}
		key := cert.KeyFile
		if key == "" {
			key = strings.Join(cert.Key, "\n")
		}
		if strings.TrimSpace(key) == "" {
			return common.NewErrorf("TLS certificate %d is missing its private key. Configure a private key file path or private key content before saving the inbound.", i+1)
		}
		hasServerCertificate = true
	}
	if !hasServerCertificate {
		return common.NewError("TLS requires a server certificate and private key. Configure an encipherment or issue certificate before saving the inbound.")
	}
	return nil
}

// finalMaskRealityTcpMasks returns the stream's finalmask.tcp masks when the
// stream uses REALITY security, or nil otherwise. A non-empty result means
// this stream carries the finalmask+REALITY combination that panics
// Xray-core (see https://github.com/XTLS/Xray-core/issues/6453): finalmask
// wraps the connection before REALITY's handshake ever sees it, and
// reality.Server() does an unchecked type assertion assuming a raw
// *net.TCPConn, which panics once finalmask is in front of it.
//
// Only finalmask.tcp matters here — TcpmaskManager (the thing that wraps the
// listener ahead of REALITY's handshake, in xray-core's own
// transport/internet/memory_settings.go) is only constructed when tcp masks
// are present; a finalmask.udp-only config never touches the TCP accept path
// REALITY runs on, so it doesn't reproduce this panic and shouldn't be
// rejected.
func finalMaskRealityTcpMasks(stream map[string]any) []any {
	if stream["security"] != "reality" {
		return nil
	}
	finalmask, ok := stream["finalmask"].(map[string]any)
	if !ok {
		return nil
	}
	tcp, _ := finalmask["tcp"].([]any)
	return tcp
}

// validateFinalMaskRealityCombo rejects finalmask.tcp configured together
// with REALITY security at save time. Upstream has confirmed this
// combination will be documented as unsupported rather than made graceful,
// so the panel must not let it be saved.
func validateFinalMaskRealityCombo(streamSettings string) error {
	if streamSettings == "" {
		return nil
	}
	var stream map[string]any
	if err := json.Unmarshal([]byte(streamSettings), &stream); err != nil {
		return nil
	}
	if len(finalMaskRealityTcpMasks(stream)) == 0 {
		return nil
	}
	return common.NewError("Finalmask is not supported with REALITY security — it crashes Xray-core on the first connection (see XTLS/Xray-core#6453). Remove the finalmask configuration or switch security to tls/none.")
}

var xmcProfileUsernamePattern = regexp.MustCompile(`^[A-Za-z0-9_]{3,16}$`)

// xmcMaskProfilesComplete reports whether an xmc finalmask carries the signed
// Minecraft session profiles xray-core has required since v26.7.28 (#6487).
// The core replaced the old `usernames` string list with `profiles` objects
// and removed the "default to Dream when empty" fallback, so a mask still on
// the legacy shape — or one whose profiles are incomplete — now fails
// conf.XMC.Build() and takes the entire config down with it rather than
// degrading that one inbound.
//
// The texture fields are a signed blob only Mojang's session server can issue
// (resolve the UUID by username, then fetch the profile with unsigned=false),
// so the panel cannot synthesize a valid profile from a legacy username; an
// incomplete mask can only be reported or dropped.
func xmcMaskProfilesComplete(mask map[string]any) bool {
	settings, ok := mask["settings"].(map[string]any)
	if !ok {
		return false
	}
	profiles, _ := settings["profiles"].([]any)
	if len(profiles) == 0 {
		return false
	}
	for _, entry := range profiles {
		profile, ok := entry.(map[string]any)
		if !ok {
			return false
		}
		username, _ := profile["username"].(string)
		if !xmcProfileUsernamePattern.MatchString(username) {
			return false
		}
		id, _ := profile["uuid"].(string)
		if _, err := uuid.Parse(id); err != nil {
			return false
		}
		if value, _ := profile["texturesValue"].(string); value == "" {
			return false
		}
		if signature, _ := profile["texturesSignature"].(string); signature == "" {
			return false
		}
	}
	return true
}

// isIncompleteXmcMask reports whether a finalmask.tcp entry is an xmc mask
// xray-core would refuse to build.
func isIncompleteXmcMask(entry any) bool {
	mask, ok := entry.(map[string]any)
	if !ok {
		return false
	}
	if maskType, _ := mask["type"].(string); maskType != "xmc" {
		return false
	}
	return !xmcMaskProfilesComplete(mask)
}

// incompleteXmcMaskCount counts the stream's xmc finalmask entries that
// xray-core would refuse to build.
func incompleteXmcMaskCount(stream map[string]any) int {
	finalmask, ok := stream["finalmask"].(map[string]any)
	if !ok {
		return 0
	}
	tcp, _ := finalmask["tcp"].([]any)
	count := 0
	for _, entry := range tcp {
		if isIncompleteXmcMask(entry) {
			count++
		}
	}
	return count
}

func stripIncompleteXmcMasks(stream map[string]any) int {
	finalmask, ok := stream["finalmask"].(map[string]any)
	if !ok {
		return 0
	}
	tcp, _ := finalmask["tcp"].([]any)
	if len(tcp) == 0 {
		return 0
	}
	kept := make([]any, 0, len(tcp))
	dropped := 0
	for _, entry := range tcp {
		if isIncompleteXmcMask(entry) {
			dropped++
			continue
		}
		kept = append(kept, entry)
	}
	if dropped == 0 {
		return 0
	}
	if len(kept) == 0 {
		delete(finalmask, "tcp")
	} else {
		finalmask["tcp"] = kept
	}
	if len(finalmask) == 0 {
		delete(stream, "finalmask")
	}
	return dropped
}

// dropEmptyRandPackets removes the leftover empty "packet" from finalmask
// items that also carry a rand, and reports how many it cleared.
//
// xray-core treats even an empty array as a packet, and every item kind is
// exclusive: noise refuses "len(item.Packet) > 0 && item.Rand.To > 0" and
// header-custom refuses "exactly one item kind must be set". Either error
// fails the whole config build, so one such item keeps every inbound offline.
// The panel's mask editor wrote that pair whenever an item was switched to the
// rand-driven array kind, so stored rows carry it; clearing an empty packet
// changes nothing about the mask the admin configured.
func dropEmptyRandPackets(node any) int {
	switch value := node.(type) {
	case map[string]any:
		cleared := 0
		if packet, ok := value["packet"].([]any); ok && len(packet) == 0 && randIsSet(value["rand"]) {
			delete(value, "packet")
			cleared++
		}
		for _, child := range value {
			cleared += dropEmptyRandPackets(child)
		}
		return cleared
	case []any:
		cleared := 0
		for _, child := range value {
			cleared += dropEmptyRandPackets(child)
		}
		return cleared
	default:
		return 0
	}
}

// randIsSet reports whether a finalmask item's rand selects a random packet.
// It is a number on header-custom items and a dash-range string on noise ones.
func randIsSet(value any) bool {
	switch rand := value.(type) {
	case float64:
		return rand > 0
	case string:
		return rand != "" && rand != "0" && rand != "0-0"
	default:
		return false
	}
}

// validateFinalMaskXmcProfiles rejects an xmc finalmask without complete
// profiles at save time, so the admin gets a targeted error instead of a core
// that refuses to start (or, after GetXrayConfig heals it, an inbound quietly
// serving without the obfuscation they configured).
func validateFinalMaskXmcProfiles(streamSettings string) error {
	if streamSettings == "" {
		return nil
	}
	var stream map[string]any
	if err := json.Unmarshal([]byte(streamSettings), &stream); err != nil {
		return nil
	}
	if incompleteXmcMaskCount(stream) == 0 {
		return nil
	}
	return common.NewError("XMC finalmask requires at least one complete Minecraft profile — each needs a username (3-16 of A-Z a-z 0-9 _), a UUID, and both texture fields from Mojang's session server (XTLS/Xray-core#6487). Complete the profiles or remove the XMC mask.")
}











// AddInbound creates a new inbound configuration.
// It validates port uniqueness, client email uniqueness, and required fields,
// then saves the inbound to the database and optionally adds it to the running Xray instance.
// Returns the created inbound, whether Xray needs restart, and any error.
func (s *InboundService) AddInbound(inbound *model.Inbound) (*model.Inbound, bool, error) {
	inbound.Id = 0
	if err := normalizeTuicSettings(inbound); err != nil {
		return inbound, false, err
	}
	inbound.TrafficResetDay = normalizeTrafficResetDay(inbound.TrafficResetDay)
	// Normalize streamSettings based on protocol
	s.normalizeStreamSettings(inbound)
	{
		if err := validateInboundTLSCertificates(inbound.StreamSettings); err != nil {
			return inbound, false, err
		}
	}
	if err := validateFinalMaskRealityCombo(inbound.StreamSettings); err != nil {
		return inbound, false, err
	}
	if err := validateFinalMaskXmcProfiles(inbound.StreamSettings); err != nil {
		return inbound, false, err
	}
	if err := s.normalizeAmneziaWGSettings(inbound, ""); err != nil {
		return inbound, false, err
	}

	inbound.SubSortIndex = normalizeSubSortIndex(inbound.SubSortIndex)
	if err := normalizeInboundShareAddressStrict(inbound); err != nil {
		return inbound, false, err
	}

	tag, err := s.resolveInboundTag(inbound, 0)
	if err != nil {
		return inbound, false, err
	}
	inbound.Tag = tag

	normalizeLegacyClientSettings(inbound)
	clients, err := s.GetClients(inbound)
	if err != nil {
		return inbound, false, err
	}
	if err := validateClientsRenewal(clients); err != nil {
		return inbound, false, err
	}
	for _, traffic := range inbound.ClientStats {
		if err := validateClientRenewal(model.Client{Reset: traffic.Reset, ResetDay: traffic.ResetDay, ResetWeekday: traffic.ResetWeekday}); err != nil {
			return inbound, false, err
		}
	}
	existEmail, err := s.clientService.checkEmailsExistForClients(s, clients)
	if err != nil {
		return inbound, false, err
	}
	if existEmail != "" {
		return inbound, false, common.NewError("Duplicate email:", existEmail)
	}

	if inbound.DisableFlow {
		if stripped, changed := stripClientFlows(inbound.Settings); changed {
			inbound.Settings = stripped
		}
		for i := range clients {
			clients[i].Flow = ""
		}
	}

	// Ensure created_at and updated_at on clients in settings
	if len(clients) > 0 {
		var settings map[string]any
		if err2 := json.Unmarshal([]byte(inbound.Settings), &settings); err2 == nil && settings != nil {
			now := time.Now().Unix() * 1000
			updatedClients := make([]model.Client, 0, len(clients))
			for _, c := range clients {
				if c.CreatedAt == 0 {
					c.CreatedAt = now
				}
				c.UpdatedAt = now
				updatedClients = append(updatedClients, c)
			}
			settings["clients"] = updatedClients
			if bs, err3 := json.MarshalIndent(settings, "", "  "); err3 == nil {
				inbound.Settings = string(bs)
			} else {
				logger.Debug("Unable to marshal inbound settings with timestamps:", err3)
			}
		} else if err2 != nil {
			logger.Debug("Unable to parse inbound settings for timestamps:", err2)
		}
	}

	// Defensively fix any Shadowsocks-2022 client PSK whose length doesn't match
	// the inbound method (e.g. an API caller supplied a wrong-size key).
	if normalized, changed := normalizeShadowsocksClientKeys(inbound.Settings); changed {
		inbound.Settings = normalized
	}

	// Secure client ID
	for _, client := range clients {
		switch inbound.Protocol {
		case "trojan":
			if client.Password == "" {
				return inbound, false, common.NewError("empty client ID")
			}
		case "shadowsocks":
			if client.Email == "" {
				return inbound, false, common.NewError("empty client ID")
			}
		case "hysteria":
			if client.Auth == "" {
				return inbound, false, common.NewError("empty client ID")
			}
		case "wireguard", "amneziawg":
			if client.PublicKey == "" {
				return inbound, false, common.NewError("wireguard client requires a key")
			}
			case "tuic":
			if client.ID == "" {
				return inbound, false, common.NewError("empty client ID")
			}
			if client.Password == "" {
				return inbound, false, common.NewError("tuic client requires a password")
			}
			if client.Email == "" {
				return inbound, false, common.NewError("empty client email")
			}
		default:
			if client.ID == "" {
				return inbound, false, common.NewError("empty client ID")
			}
		}
	}

	needRestart := false
	var postCommitApply func()
	err = runSerializedTx(func(tx *gorm.DB) error {
		conflict, cErr := checkPortConflictTx(tx, inbound, 0)
		if cErr != nil {
			return cErr
		}
		if conflict != nil {
			return common.NewError(conflict.String())
		}
		if err := tx.Omit("ClientStats").Save(inbound).Error; err != nil {
			return err
		}
		// The relay port is derived from the id, only known after Save, and only a
		// local row owns one: checkPortConflictTx ran no relay check with ignoreId==0.
		if inbound.Protocol == model.AmneziaWG {
			if self := amneziawgnetSocksSelfConflict(inbound, inbound.Id); self != "" {
				return common.NewError(self)
			}
			conflict, cErr := checkAmneziawgnetSocksRelayCollision(tx, inbound.Id)
			if cErr != nil {
				return cErr
			}
			if conflict != nil {
				return common.NewError(conflict.String())
			}
			conflict, cErr = checkAmneziawgnetSocksReverseConflict(tx, inbound.Id)
			if cErr != nil {
				return cErr
			}
			if conflict != nil {
				return common.NewError(conflict.String())
			}
			// The clients' forward specs were validated while this row had no id,
			// so the ports it now derives were never in the guard's context.
			if aErr := s.checkAmneziaWGForwardedPorts(tx, inbound.Settings); aErr != nil {
				return aErr
			}
		}
		if inbound.Protocol == model.TUIC {
			if self := tuicSocksSelfConflict(inbound, inbound.Id); self != "" {
				return common.NewError(self)
			}
			conflict, cErr := checkTuicSocksRelayCollision(tx, inbound.Id)
			if cErr != nil {
				return cErr
			}
			if conflict != nil {
				return common.NewError(conflict.String())
			}
			conflict, cErr = checkTuicSocksReverseConflict(tx, inbound.Id)
			if cErr != nil {
				return cErr
			}
			if conflict != nil {
				return common.NewError(conflict.String())
			}
		}
		// Emails seeded here (import's ClientStats, e.g. the controller's forced
		// Enable=true on every imported stat row) are authoritative for this call
		// and must not be clobbered by the AddClientStat loop below, which derives
		// its enable/total/expiry/reset from Settings.clients[] instead — a second,
		// possibly-stale source for the same columns on a plain (non-import) create.
		statEmails := make(map[string]bool, len(inbound.ClientStats))
		for i := range inbound.ClientStats {
			if inbound.ClientStats[i].Email == "" {
				continue
			}
			statEmails[inbound.ClientStats[i].Email] = true
			inbound.ClientStats[i].Id = 0
			inbound.ClientStats[i].InboundId = inbound.Id
			if err := tx.Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "email"}},
				DoNothing: true,
			}).Create(&inbound.ClientStats[i]).Error; err != nil {
				return err
			}
		}
		for _, client := range clients {
			if statEmails[client.Email] {
				continue
			}
			if err := s.AddClientStat(tx, inbound.Id, &client); err != nil {
				return err
			}
		}
		if err := s.clientService.SyncInbound(tx, inbound.Id, clients); err != nil {
			return err
		}

		if inbound.Enable {
			{
				rt, push, _, perr := s.localRuntimePlan(inbound)
				if perr != nil {
					return perr
				}
				if push {
					payload := inbound
					pushable := true
					if inbound.Protocol == model.TUIC {
						if built, bErr := s.buildInboundForLocalRuntime(tx, inbound); bErr == nil {
							payload = built
						} else {
							logger.Debug("Unable to prepare runtime inbound config:", bErr)
							pushable = false
						}
					}
					if pushable {
						postCommitApply = func() {
							if err1 := rt.AddInbound(context.Background(), payload); err1 == nil {
								logger.Debug("New inbound added on", rt.Name(), ":", inbound.Tag)
							} else {
								logger.Debug("Unable to add inbound on", rt.Name(), ":", err1)
								if inbound.Protocol != model.TUIC {
									needRestart = true
								}
							}
						}
					}
				}
			}
		}

		return nil
	})
	if err != nil {
		return inbound, false, err
	}
	if postCommitApply != nil {
		postCommitApply()
	}

	// A routed mtproto inbound is not an Xray inbound itself, so the runtime
	// push above only (re)starts the mtg sidecar. The egress SOCKS bridge lives
	// in the generated config, so force a regen to wire it in.
	

	return inbound, needRestart, err
}

func (s *InboundService) DelInbound(id int) (bool, error) { return s.delInbound(id) }

func (s *InboundService) delInbound(id int) (bool, error) {
	db := database.GetDB()

	needRestart := false
	var postCommitApply func()
	var ib model.Inbound
	loadErr := db.Model(model.Inbound{}).Where("id = ?", id).First(&ib).Error
	if loadErr == nil {
		shouldPushToRuntime := ib.Enable
		if shouldPushToRuntime {
			{
				rt, push, _, perr := s.localRuntimePlan(&ib)
				if perr != nil {
					logger.Warning("DelInbound: runtime lookup failed, deleting central row anyway:", perr)
				} else if push {
					postCommitApply = func() {
						if err1 := rt.DelInbound(context.Background(), &ib); err1 == nil {
							logger.Debug("Inbound deleted on", rt.Name(), ":", ib.Tag)
						} else {
							logger.Warning("DelInbound on", rt.Name(), "failed after commit:", err1)
							needRestart = true
						}
					}
				} else {
					needRestart = true
				}
			}
		} else {
			logger.Debug("DelInbound: skipping runtime push for disabled local inbound id:", id)
		}
	} else {
		logger.Debug("DelInbound: inbound not found, id:", id)
	}

	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := s.clientService.DetachInbound(tx, id); err != nil {
			return err
		}
		if err := tx.Delete(model.Inbound{}, id).Error; err != nil {
			return err
		}

		// Drop the deleted inbound from any sub-balancer that selects it; a
		// dangling id would emit a member no subscriber can resolve (#5648).
		var balancers []model.SubBalancer
		if err := tx.Find(&balancers).Error; err != nil {
			return err
		}
		for i := range balancers {
			before := balancers[i].InboundIds
			balancers[i].InboundIds = slices.DeleteFunc(before, func(b int) bool { return b == id })
			if len(balancers[i].InboundIds) == len(before) {
				continue
			}
			if err := tx.Save(&balancers[i]).Error; err != nil {
				return err
			}
		}

		return nil
	}); err != nil {
		return needRestart, err
	}
	if postCommitApply != nil {
		postCommitApply()
	}
	if loadErr == nil && ib.Tag != "" {
		if routingChanged, syncErr := (&XraySettingService{}).RemoveInboundTagReferences(ib.Tag); syncErr != nil {
			logger.Warning("DelInbound: sync routing on inbound delete failed:", syncErr)
		} else if routingChanged {
			needRestart = true
		}
	}
	{
		var count int64
		if err := db.Model(&model.Inbound{}).Count(&count).Error; err != nil {
			return needRestart, err
		}
		if count == 0 {
			if err := db.Exec("DELETE FROM sqlite_sequence WHERE name = ?", "inbounds").Error; err != nil {
				return needRestart, err
			}
		}
	}
	// Drop the egress SOCKS bridge a routed mtproto inbound left in the config.
	
	return needRestart, nil
}

type BulkDelInboundResult struct {
	Deleted int                    `json:"deleted"`
	Skipped []BulkDelInboundReport `json:"skipped,omitempty"`
}

type BulkDelInboundReport struct {
	Id     int    `json:"id"`
	Reason string `json:"reason"`
}

// DelInbounds removes every inbound in the list, reusing the single-delete
// path per id. Failures are recorded in Skipped and processing continues for
// the rest; the aggregated needRestart is returned so the caller restarts
// xray at most once.
func (s *InboundService) DelInbounds(ids []int) (BulkDelInboundResult, bool, error) {
	result := BulkDelInboundResult{}
	needRestart := false
	for _, id := range ids {
		r, err := s.delInbound(id)
		if err != nil {
			result.Skipped = append(result.Skipped, BulkDelInboundReport{Id: id, Reason: err.Error()})
			continue
		}
		result.Deleted++
		if r {
			needRestart = true
		}
	}
	return result, needRestart, nil
}

func (s *InboundService) GetInbound(id int) (*model.Inbound, error) {
	db := database.GetDB()
	inbound := &model.Inbound{}
	err := db.Model(model.Inbound{}).First(inbound, id).Error
	if err != nil {
		return nil, err
	}
	return inbound, nil
}

func (s *InboundService) GetInboundDetail(id int) (*model.Inbound, error) {
	db := database.GetDB()
	inbound := &model.Inbound{}
	err := db.Model(model.Inbound{}).Preload("ClientStats").First(inbound, id).Error
	if err != nil {
		return nil, err
	}
	s.enrichClientStats(db, []*model.Inbound{inbound})

	return inbound, nil
}

// SetInboundSubSortIndex changes only the subscription sort order, so a
// reorder cannot carry a stale settings/client payload over another edit.
func (s *InboundService) SetInboundSubSortIndex(id int, index int) error {
	index = normalizeSubSortIndex(index)
	inbound, err := s.GetInbound(id)
	if err != nil {
		return err
	}
	if inbound.SubSortIndex == index {
		return nil
	}

	db := database.GetDB()
	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(model.Inbound{}).Where("id = ?", id).
			Update("sub_sort_index", index).Error; err != nil {
			return err
		}

		return nil
	}); err != nil {
		return err
	}
	inbound.SubSortIndex = index

	{
		return nil
	}
}

func (s *InboundService) SetInboundEnable(id int, enable bool) (bool, error) {
	inbound, err := s.GetInbound(id)
	if err != nil {
		return false, err
	}
	if inbound.Enable == enable {
		return false, nil
	}

	db := database.GetDB()
	// Enabling puts this row's ports into the running config, and the guards ran
	// only if it was saved: a restored or hand-edited row reaches it unchecked.
	if enable {
		conflict, err := checkPortConflictTx(db, inbound, inbound.Id)
		if err != nil {
			return false, err
		}
		if conflict != nil {
			return false, common.NewError(conflict.String())
		}
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(model.Inbound{}).Where("id = ?", id).
			Update("enable", enable).Error; err != nil {
			return err
		}

		return nil
	}); err != nil {
		return false, err
	}
	inbound.Enable = enable

	needRestart := false
	rt, push, _, perr := s.localRuntimePlan(inbound)
	if perr != nil {
		return false, perr
	}

	

	if !push {
		return true, nil
	}

	if err := rt.DelInbound(context.Background(), inbound); err != nil &&
		!strings.Contains(err.Error(), "not found") {
		logger.Debug("SetInboundEnable: DelInbound on", rt.Name(), "failed:", err)
		needRestart = true
	}
	if !enable {
		return needRestart, nil
	}

	runtimeInbound, err := s.buildInboundForLocalRuntime(db, inbound)
	if err != nil {
		logger.Debug("SetInboundEnable: build runtime config failed:", err)
		return true, nil
	}
	if err := rt.AddInbound(context.Background(), runtimeInbound); err != nil {
		logger.Debug("SetInboundEnable: AddInbound on", rt.Name(), "failed:", err)
		needRestart = true
	}
	return needRestart, nil
}

func (s *InboundService) validateUpdatedInboundClients(inbound *model.Inbound) error {
	clients, err := s.GetClients(inbound)
	if err != nil {
		return err
	}
	if err := validateClientsRenewal(clients); err != nil {
		return err
	}
	for _, client := range clients {
		switch inbound.Protocol {
		case model.Hysteria:
			if client.Auth == "" {
				return common.NewError("empty client ID")
			}
		case model.TUIC:
			if client.ID == "" {
				return common.NewError("empty client ID")
			}
			if client.Password == "" {
				return common.NewError("tuic client requires a password")
			}
			if client.Email == "" {
				return common.NewError("empty client email")
			}
		}
	}
	return nil
}

func (s *InboundService) UpdateInbound(inbound *model.Inbound) (*model.Inbound, bool, error) {
	inbound.TrafficResetDay = normalizeTrafficResetDay(inbound.TrafficResetDay)
	// Normalize streamSettings based on protocol
	s.normalizeStreamSettings(inbound)
	if err := validateFinalMaskRealityCombo(inbound.StreamSettings); err != nil {
		return inbound, false, err
	}
	if err := validateFinalMaskXmcProfiles(inbound.StreamSettings); err != nil {
		return inbound, false, err
	}

	oldInbound, err := s.GetInbound(inbound.Id)
	if err != nil {
		return inbound, false, err
	}

	if err := normalizeTuicSettings(inbound); err != nil {
		return inbound, false, err
	}
	if err := s.normalizeAmneziaWGSettings(inbound, oldInbound.Settings); err != nil {
		return inbound, false, err
	}
	inbound.SubSortIndex = normalizeSubSortIndex(inbound.SubSortIndex)

	{
		if err := validateInboundTLSCertificates(inbound.StreamSettings); err != nil {
			if validateInboundTLSCertificates(oldInbound.StreamSettings) == nil {
				return inbound, false, err
			}
		}
	}

	// Capture the pre-edit protocol and routing state before oldInbound is
	// overwritten with the new values further down, then ensure a routed
	// inbound keeps a stable egress port (reusing the one already stored).
	oldProtocol := oldInbound.Protocol

	tag := oldInbound.Tag
	oldBits := inboundTransports(oldInbound.Protocol, oldInbound.StreamSettings, oldInbound.Settings)
	oldTagWasAuto := isAutoGeneratedTag(tag, oldInbound.Port, oldBits)

	needRestart := false
	var postCommitApply func()

	txErr := runSerializedTx(func(tx *gorm.DB) error {
		// Re-read inside the writer: a traffic tick since the read above moved the
		// counters and may have renewed or disabled clients.
		stored := &model.Inbound{}
		if err := tx.First(stored, inbound.Id).Error; err != nil {
			return err
		}
		oldInbound = stored

		{
			inbound.Settings = keepStoredClients(inbound.Settings, stored.Settings)
			inbound.Enable = stored.Enable
		}
		// On the clients actually saved: a protocol switch keeps the stored ones.
		if err := s.validateUpdatedInboundClients(inbound); err != nil {
			return err
		}
		conflict, cErr := checkPortConflictTx(tx, inbound, inbound.Id)
		if cErr != nil {
			return cErr
		}
		if conflict != nil {
			return common.NewError(conflict.String())
		}
		if err := s.updateClientTraffics(tx, oldInbound, inbound); err != nil {
			return err
		}

		// Ensure created_at and updated_at exist in inbound.Settings clients
		{
			var oldSettings map[string]any
			_ = json.Unmarshal([]byte(oldInbound.Settings), &oldSettings)
			emailToCreated := map[string]int64{}
			emailToUpdated := map[string]int64{}
			if oldSettings != nil {
				if oc, ok := oldSettings["clients"].([]any); ok {
					for _, it := range oc {
						if m, ok2 := it.(map[string]any); ok2 {
							if email, ok3 := m["email"].(string); ok3 {
								switch v := m["created_at"].(type) {
								case float64:
									emailToCreated[email] = int64(v)
								case int64:
									emailToCreated[email] = v
								}
								switch v := m["updated_at"].(type) {
								case float64:
									emailToUpdated[email] = int64(v)
								case int64:
									emailToUpdated[email] = v
								}
							}
						}
					}
				}
			}
			var newSettings map[string]any
			if err2 := json.Unmarshal([]byte(inbound.Settings), &newSettings); err2 == nil && newSettings != nil {
				now := time.Now().Unix() * 1000
				if nSlice, ok := newSettings["clients"].([]any); ok {
					for i := range nSlice {
						if m, ok2 := nSlice[i].(map[string]any); ok2 {
							email, _ := m["email"].(string)
							if _, ok3 := m["created_at"]; !ok3 {
								if v, ok4 := emailToCreated[email]; ok4 && v > 0 {
									m["created_at"] = v
								} else {
									m["created_at"] = now
								}
							}
							// Preserve client's updated_at if present; do not bump on parent inbound update
							if _, hasUpdated := m["updated_at"]; !hasUpdated {
								if v, ok4 := emailToUpdated[email]; ok4 && v > 0 {
									m["updated_at"] = v
								}
							}
							nSlice[i] = m
						}
					}
					newSettings["clients"] = nSlice
					if bs, err3 := json.MarshalIndent(newSettings, "", "  "); err3 == nil {
						inbound.Settings = string(bs)
					}
				}
			}
		}

		// A Shadowsocks-2022 method change resizes the key, but existing client PSKs
		// keep their old length and would be rejected by xray. Regenerate mismatched
		// client keys so the inbound stays connectable.
		if normalized, changed := normalizeShadowsocksClientKeys(inbound.Settings); changed {
			inbound.Settings = normalized
			logger.Warning("Shadowsocks inbound", inbound.Id, "method change resized keys; regenerated mismatched client PSK(s)")
		}

		// Re-gate Vision flow now that the new stream/encryption is known: if this
		// VLESS inbound just became flow-eligible (e.g. vlessenc was enabled on an
		// XHTTP inbound), restore Vision for clients whose intended flow is Vision
		// but was stripped while the inbound was ineligible.
		if !inbound.DisableFlow {
			if restored, changed := s.restoreVisionFlowForEligibleInbound(tx, inbound.Settings, inbound.StreamSettings, inbound.Protocol); changed {
				inbound.Settings = restored
			}
		} else {
			if stripped, changed := stripClientFlows(inbound.Settings); changed {
				inbound.Settings = stripped
			}
		}

		oldInbound.Total = inbound.Total
		oldInbound.Remark = inbound.Remark
		oldInbound.SubSortIndex = inbound.SubSortIndex
		oldInbound.ExcludeFromSub = inbound.ExcludeFromSub
		oldInbound.Enable = inbound.Enable
		oldInbound.ExpiryTime = inbound.ExpiryTime
		oldInbound.TrafficReset = inbound.TrafficReset
		oldInbound.TrafficResetDay = inbound.TrafficResetDay
		oldInbound.Listen = inbound.Listen
		oldInbound.Port = inbound.Port
		oldInbound.Protocol = inbound.Protocol
		oldInbound.DisableFlow = inbound.DisableFlow
		oldInbound.Settings = inbound.Settings
		oldInbound.StreamSettings = inbound.StreamSettings
		oldInbound.Sniffing = inbound.Sniffing
		if strings.TrimSpace(inbound.ShareAddrStrategy) == "" {
			normalizeInboundShareAddress(oldInbound)
			inbound.ShareAddrStrategy = oldInbound.ShareAddrStrategy
			inbound.ShareAddr = oldInbound.ShareAddr
		} else {
			if err := normalizeInboundShareAddressStrict(inbound); err != nil {
				return err
			}
			oldInbound.ShareAddrStrategy = inbound.ShareAddrStrategy
			oldInbound.ShareAddr = inbound.ShareAddr

		}
		if oldTagWasAuto && inbound.Tag == tag {
			inbound.Tag = ""
		}
		resolvedTag, err := s.resolveInboundTag(inbound, inbound.Id)
		if err != nil {
			return err
		}
		oldInbound.Tag = resolvedTag
		inbound.Tag = oldInbound.Tag

		{
			rt, push, _, perr := s.localRuntimePlan(oldInbound)
			if perr != nil {
				return perr
			}
			if !push {
				needRestart = true
			} else if oldProtocol == model.TUIC || oldInbound.Protocol == model.TUIC {
				oldSnapshot := *oldInbound
				oldSnapshot.Tag = tag
				oldSnapshot.Protocol = oldProtocol
				payload := oldInbound
				pushable := true
				if inbound.Enable {
					if built, err2 := s.buildInboundForLocalRuntime(tx, oldInbound); err2 == nil {
						payload = built
					} else {
						logger.Debug("Unable to prepare runtime inbound config:", err2)
						pushable = false
					}
				}
				newProtocolIsSidecar := oldInbound.Protocol == model.TUIC
				if pushable {
					postCommitApply = func() {
						if err2 := rt.UpdateInbound(context.Background(), &oldSnapshot, payload); err2 == nil {
							logger.Debug("Updated inbound applied on", rt.Name(), ":", oldInbound.Tag)
						} else {
							logger.Debug("Unable to update inbound on", rt.Name(), ":", err2)
							if !newProtocolIsSidecar {
								needRestart = true
							}
						}
					}
				}
			} else {
				oldSnapshot := *oldInbound
				oldSnapshot.Tag = tag
				var runtimeInbound *model.Inbound
				if inbound.Enable {
					var err2 error
					runtimeInbound, err2 = s.buildInboundForLocalRuntime(tx, oldInbound)
					if err2 != nil {
						logger.Debug("Unable to prepare runtime inbound config:", err2)
						needRestart = true
					}
				}
				postCommitApply = func() {
					if err2 := rt.DelInbound(context.Background(), &oldSnapshot); err2 == nil {
						logger.Debug("Old inbound deleted on", rt.Name(), ":", tag)
					}
					if runtimeInbound == nil {
						return
					}
					if err2 := rt.AddInbound(context.Background(), runtimeInbound); err2 == nil {
						logger.Debug("Updated inbound added on", rt.Name(), ":", oldInbound.Tag)
					} else {
						logger.Debug("Unable to update inbound on", rt.Name(), ":", err2)
						needRestart = true
					}
				}
			}
		}

		if err := tx.Save(oldInbound).Error; err != nil {
			return err
		}
		newClients, gcErr := s.GetClients(oldInbound)
		if gcErr != nil {
			return gcErr
		}
		if err := s.clientService.SyncInbound(tx, oldInbound.Id, newClients); err != nil {
			return err
		}

		// (Re)generate the Xray config whenever routing was or is now enabled, so
		// the egress SOCKS bridge is added, moved, or dropped to match the new
		// settings.
		
		return nil
	})
	if txErr != nil {
		return inbound, false, txErr
	}
	if postCommitApply != nil {
		postCommitApply()
	}
	// After the rename is committed, point any routing rules / loopback outbounds
	// in xrayTemplateConfig at the new tag (oldInbound.Tag now holds the resolved
	// new tag; tag holds the pre-edit one). Done post-commit so a sync failure
	// can't roll back the inbound edit.
	if tag != oldInbound.Tag {
		if routingChanged, syncErr := (&XraySettingService{}).PropagateInboundTagRename(tag, oldInbound.Tag); syncErr != nil {
			logger.Warning("UpdateInbound: sync routing on tag rename failed:", syncErr)
		} else if routingChanged {
			needRestart = true
		}
	}
	return inbound, needRestart, nil
}

func (s *InboundService) buildInboundWithClients(tx *gorm.DB, inbound *model.Inbound) (*model.Inbound, error) {
	if inbound == nil {
		return nil, fmt.Errorf("inbound is nil")
	}

	built := *inbound
	settings := map[string]any{}
	if err := json.Unmarshal([]byte(inbound.Settings), &settings); err != nil {
		return nil, err
	}

	if !inboundCanHostFallbacks(inbound) {
		return &built, nil
	}
	fallbacks, err := s.fallbackService.BuildFallbacksJSON(tx, inbound.Id)
	if err != nil {
		return nil, err
	}
	if len(fallbacks) == 0 {
		return &built, nil
	}
	generic := make([]any, 0, len(fallbacks))
	for _, f := range fallbacks {
		generic = append(generic, f)
	}
	settings["fallbacks"] = generic

	modifiedSettings, mErr := json.MarshalIndent(settings, "", "  ")
	if mErr != nil {
		return nil, mErr
	}
	built.Settings = string(modifiedSettings)
	return &built, nil
}

func (s *InboundService) buildInboundForLocalRuntime(tx *gorm.DB, inbound *model.Inbound) (*model.Inbound, error) {
	built, err := s.buildInboundWithClients(tx, inbound)
	if err != nil {
		return nil, err
	}

	settings := map[string]any{}
	if err := json.Unmarshal([]byte(built.Settings), &settings); err != nil {
		return nil, err
	}
	clients, ok := settings["clients"].([]any)
	if !ok {
		return built, nil
	}

	emails := make([]string, 0, len(clients))
	for _, client := range clients {
		if c, ok := client.(map[string]any); ok {
			email, _ := c["email"].(string)
			emails = append(emails, email)
		}
	}
	disabled, err := trafficDisabledEmails(tx, emails)
	if err != nil {
		return nil, err
	}

	trafficIDs := make(map[string]int)
	if inbound.Protocol == model.TUIC {
		var rows []xray.ClientTraffic
		if err := tx.Select("id", "email").Where("email IN ?", emails).Find(&rows).Error; err != nil {
			return nil, err
		}
		for _, row := range rows {
			trafficIDs[row.Email] = row.Id
		}
	}
	finalClients := make([]any, 0, len(clients))
	for _, client := range clients {
		c, ok := client.(map[string]any)
		if !ok {
			continue
		}
		email, _ := c["email"].(string)
		if _, off := disabled[email]; off {
			continue
		}
		if manualEnable, ok := c["enable"].(bool); ok && !manualEnable {
			continue
		}
		if inbound.Protocol == model.TUIC {
			delete(c, "traffic_id")
			if id := trafficIDs[email]; id > 0 {
				c["traffic_id"] = id
			}
		}
		finalClients = append(finalClients, c)
	}
	settings["clients"] = finalClients

	modifiedSettings, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return nil, err
	}
	runtimeInbound := *built
	runtimeInbound.Settings = string(modifiedSettings)
	return &runtimeInbound, nil
}

// updateClientTraffics syncs the ClientTraffic rows with the inbound's clients
// list: removes rows for emails that disappeared, inserts rows for newly-added
// emails. Uses sets for O(N) lookup — the previous nested-loop implementation
// was O(N²) and degraded into multi-second pauses on inbounds with thousands
// of clients (toggling, saving, or deleting any such inbound felt frozen).
func (s *InboundService) updateClientTraffics(tx *gorm.DB, oldInbound *model.Inbound, newInbound *model.Inbound) error {
	oldClients, err := s.GetClients(oldInbound)
	if err != nil {
		return err
	}
	newClients, err := s.GetClients(newInbound)
	if err != nil {
		return err
	}

	// Email is the unique key for ClientTraffic rows. Clients without an
	// email have no stats row to sync — skip them on both sides instead of
	// risking a unique-constraint hit or accidental delete of an unrelated row.
	oldEmails := make(map[string]struct{}, len(oldClients))
	for i := range oldClients {
		if oldClients[i].Email == "" {
			continue
		}
		oldEmails[oldClients[i].Email] = struct{}{}
	}
	newEmails := make(map[string]struct{}, len(newClients))
	for i := range newClients {
		if newClients[i].Email == "" {
			continue
		}
		newEmails[newClients[i].Email] = struct{}{}
	}

	// Drop stats rows for removed emails — but not when a sibling inbound
	// still references the email, since the row is the shared accumulator.
	for i := range oldClients {
		email := oldClients[i].Email
		if email == "" {
			continue
		}
		if _, kept := newEmails[email]; kept {
			continue
		}
		stillUsed, err := s.emailUsedByOtherInbounds(email, oldInbound.Id)
		if err != nil {
			return err
		}
		if stillUsed {
			continue
		}
		if err := s.DelClientStat(tx, email); err != nil {
			return err
		}
		// Keep inbound_client_ips in sync when the inbound edit drops an
		// email, so the IP-limit job doesn't keep a ghost tracking row (#4963).
		if err := s.DelClientIPs(tx, email); err != nil {
			return err
		}
	}
	for i := range newClients {
		email := newClients[i].Email
		if email == "" {
			continue
		}
		if _, existed := oldEmails[email]; existed {
			if err := s.UpdateClientStat(tx, email, &newClients[i]); err != nil {
				return err
			}
			continue
		}
		if err := s.AddClientStat(tx, oldInbound.Id, &newClients[i]); err != nil {
			return err
		}
	}
	return nil
}

func (s *InboundService) GetInboundTags() (string, error) {
	db := database.GetDB()
	var inboundTags []string
	err := db.Model(model.Inbound{}).Select("tag").Find(&inboundTags).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return "", err
	}
	tags, _ := json.Marshal(inboundTags)
	return string(tags), nil
}

func (s *InboundService) GetClientReverseTags() (string, error) {
	db := database.GetDB()
	var inbounds []model.Inbound
	err := db.Model(model.Inbound{}).Select("settings").Where("protocol = ?", "vless").Find(&inbounds).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return "[]", err
	}

	tagSet := make(map[string]struct{})
	for _, inbound := range inbounds {
		var settings map[string]any
		if err := json.Unmarshal([]byte(inbound.Settings), &settings); err != nil {
			continue
		}
		clients, ok := settings["clients"].([]any)
		if !ok {
			continue
		}
		for _, client := range clients {
			clientMap, ok := client.(map[string]any)
			if !ok {
				continue
			}
			reverse, ok := clientMap["reverse"].(map[string]any)
			if !ok {
				continue
			}
			tag, _ := reverse["tag"].(string)
			tag = strings.TrimSpace(tag)
			if tag != "" {
				tagSet[tag] = struct{}{}
			}
		}
	}

	rawTags := make([]string, 0, len(tagSet))
	for tag := range tagSet {
		rawTags = append(rawTags, tag)
	}
	sort.Strings(rawTags)

	result, _ := json.Marshal(rawTags)
	return string(result), nil
}

func (s *InboundService) SearchInbounds(query string) ([]*model.Inbound, error) {
	db := database.GetDB()
	var inbounds []*model.Inbound
	err := db.Model(model.Inbound{}).Preload("ClientStats").Where("remark like ?", "%"+query+"%").Find(&inbounds).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	return inbounds, nil
}
