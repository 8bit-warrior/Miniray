package sub

import (
	"encoding/json"

	"github.com/8bit-warrior/Miniray/internal/database/model"
)

// externalProxyMuxOverride returns an endpoint’s muxParams when it is valid JSON, else "".
// Used to override the JSON outbound's mux for that host.
func externalProxyMuxOverride(ep map[string]any) string {
	mp, ok := ep["muxParams"].(string)
	if ok && mp != "" && json.Valid([]byte(mp)) {
		return mp
	}
	return ""
}

// Apply endpoint transport, TLS and free-form stream overrides.
func applyExternalProxyStreamOverrides(ep map[string]any, stream map[string]any) {
	if hh, ok := ep["hostHeader"].(string); ok && hh != "" {
		for _, key := range []string{"wsSettings", "httpupgradeSettings", "xhttpSettings"} {
			if ts, ok := stream[key].(map[string]any); ok && ts != nil {
				ts["host"] = hh
			}
		}
	}
	if p, ok := ep["path"].(string); ok && p != "" {
		for _, key := range []string{"wsSettings", "httpupgradeSettings", "xhttpSettings"} {
			if ts, ok := stream[key].(map[string]any); ok && ts != nil {
				ts["path"] = p
			}
		}
	}
	if sp, ok := ep["sockoptParams"].(string); ok && sp != "" {
		var sockopt map[string]any
		if json.Unmarshal([]byte(sp), &sockopt) == nil && len(sockopt) > 0 {
			stream["sockopt"] = sockopt
		}
	}
	// Endpoint finalmask: merge the endpoint’s masks into the stream's finalmask (the
	// JSON renderer consumes streamSettings["finalmask"]; clash ignores it).
	if fm, ok := ep["finalMask"].(string); ok && fm != "" {
		var masks map[string]any
		if json.Unmarshal([]byte(fm), &masks) == nil && len(masks) > 0 {
			merged := mergeFinalMask(stream["finalmask"], masks)
			if len(merged) > 0 {
				stream["finalmask"] = merged
			}
		}
	}
	// Reality SNI override: the stream is already in client form, and xray
	// refuses a reality client carrying the server-side serverNames list (#6690).
	if hasEndpointOverrides(ep) {
		if sec, _ := stream["security"].(string); sec == "reality" {
			if rs, ok := stream["realitySettings"].(map[string]any); ok && rs != nil {
				if sni, ok := externalProxySNI(ep); ok {
					rs["serverName"] = sni
				}
			}
		}
	}
}

// Keep explicit endpoint remarks; otherwise use the configured remark template.
func (s *SubService) endpointRemark(inbound *model.Inbound, email string, ep map[string]any, transport string) string {
	if ep != nil {
		if final, _ := ep["remarkFinal"].(bool); final {
			r, _ := ep["remark"].(string)
			return r
		}
	}
	var extra string
	if ep != nil {
		extra, _ = ep["remark"].(string)
	}
	return s.genRemark(inbound, email, extra, transport)
}

// Override existing transport header and path fields for an advertised endpoint.
func applyEndpointHostPath(e ShareEndpoint, params map[string]string) {
	if e.ep == nil {
		return
	}
	if h, ok := e.ep["hostHeader"].(string); ok && h != "" {
		if _, exists := params["host"]; exists {
			params["host"] = h
		}
	}
	if p, ok := e.ep["path"].(string); ok && p != "" {
		if _, exists := params["path"]; exists {
			params["path"] = p
		}
	}
}

// hasEndpointOverrides enables explicit per-endpoint overrides.
func hasEndpointOverrides(ep map[string]any) bool {
	v, _ := ep["endpointOverrides"].(bool)
	return v
}

// Apply endpoint SNI and fingerprint while retaining inbound Reality keys.
func applyEndpointRealityParams(e ShareEndpoint, params map[string]string, security string) {
	if security != "reality" || e.ep == nil || !hasEndpointOverrides(e.ep) {
		return
	}
	if sni, ok := externalProxySNI(e.ep); ok {
		params["sni"] = sni
	}
	if fp, ok := e.ep["fingerprint"].(string); ok && fp != "" {
		params["fp"] = fp
	}
}

// Apply explicit endpoint certificate verification settings to encrypted links.
func applyEndpointAllowInsecure(e ShareEndpoint, params map[string]string, security string) {
	if e.ep == nil || security == "none" {
		return
	}
	if ai, ok := e.ep["allowInsecure"].(bool); ok && ai {
		params["allowInsecure"] = "1"
	}
}

// applyEndpointFinalMask merges an endpoint’s Final Mask into the raw link's fm
// param, mirroring the applyExternalProxyStreamOverrides merge on the JSON/Clash path.
func applyEndpointFinalMask(e ShareEndpoint, params map[string]string) {
	if merged, ok := endpointFinalMask(e, params["fm"]); ok {
		params["fm"] = merged
	}
}

// applyEndpointFinalMaskObj is applyEndpointFinalMask for the VMess object form.
func applyEndpointFinalMaskObj(e ShareEndpoint, obj map[string]any) {
	baseFm, _ := obj["fm"].(string)
	if merged, ok := endpointFinalMask(e, baseFm); ok {
		obj["fm"] = merged
	}
}

func endpointFinalMask(e ShareEndpoint, baseFm string) (string, bool) {
	if e.ep == nil {
		return "", false
	}
	fm, ok := e.ep["finalMask"].(string)
	if !ok || fm == "" {
		return "", false
	}
	var masks map[string]any
	if json.Unmarshal([]byte(fm), &masks) != nil || len(masks) == 0 {
		return "", false
	}
	var base any
	if baseFm != "" {
		var baseMap map[string]any
		if json.Unmarshal([]byte(baseFm), &baseMap) == nil {
			base = baseMap
		}
	}
	return marshalFinalMask(mergeFinalMask(base, masks))
}

// applyEndpointHostPathObj is applyEndpointHostPath for the VMess object form.
func applyEndpointHostPathObj(e ShareEndpoint, obj map[string]any) {
	if e.ep == nil {
		return
	}
	if h, ok := e.ep["hostHeader"].(string); ok && h != "" {
		if _, exists := obj["host"]; exists {
			obj["host"] = h
		}
	}
	if p, ok := e.ep["path"].(string); ok && p != "" {
		if _, exists := obj["path"]; exists {
			obj["path"] = p
		}
	}
}

func (s *SubService) prepareExternalProxies(stream map[string]any, inbound *model.Inbound, format string) {
	entries, _ := stream["externalProxy"].([]any)
	if len(entries) == 0 {
		return
	}
	selected := make([]any, 0, len(entries))
	for _, entry := range entries {
		ep, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		excluded := false
		if formats, ok := ep["excludeFromSubTypes"].([]any); ok {
			for _, value := range formats {
				if value == format {
					excluded = true
				}
			}
		}
		if excluded {
			continue
		}
		if dest, _ := ep["dest"].(string); dest == "" {
			ep["dest"] = s.resolveInboundAddress(inbound)
		}
		if port, _ := ep["port"].(float64); port == 0 {
			ep["port"] = float64(inbound.Port)
		}
		selected = append(selected, ep)
	}
	if len(selected) == 0 {
		delete(stream, "externalProxy")
	} else {
		stream["externalProxy"] = selected
	}
}
