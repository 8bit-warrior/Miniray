package maskcompat

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// PinnedConfigJSON renders canonical XDNS objects for the bundled v26.6.27 core.
func PinnedConfigJSON(data []byte) ([]byte, error) {
	var value any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	changed, err := rewritePinnedXdns(value)
	if err != nil {
		return nil, err
	}
	if !changed {
		return data, nil
	}
	return json.MarshalIndent(value, "", "  ")
}

func rewritePinnedXdns(value any) (bool, error) {
	changed := false
	switch value := value.(type) {
	case map[string]any:
		if kind, _ := value["type"].(string); strings.EqualFold(kind, "xdns") {
			settings, _ := value["settings"].(map[string]any)
			updated, err := pinnedXdnsSettings(settings)
			if err != nil {
				return false, err
			}
			changed = updated
		}
		for _, child := range value {
			updated, err := rewritePinnedXdns(child)
			if err != nil {
				return false, err
			}
			changed = changed || updated
		}
	case []any:
		for _, child := range value {
			updated, err := rewritePinnedXdns(child)
			if err != nil {
				return false, err
			}
			changed = changed || updated
		}
	}
	return changed, nil
}

func pinnedXdnsSettings(settings map[string]any) (bool, error) {
	domains, _ := settings["domains"].([]any)
	resolvers, _ := settings["resolvers"].([]any)
	if value, exists := settings["extraPoll"]; exists && fmt.Sprint(value) != "0" {
		return false, fmt.Errorf("xdns: extraPoll requires a newer core than v26.6.27")
	}
	changed := false
	specs := []any{}
	for _, domain := range domains {
		if legacy, ok := domain.(string); ok {
			specs = append(specs, legacy)
			continue
		}
		entry, ok := domain.(map[string]any)
		if !ok {
			return false, fmt.Errorf("xdns: invalid domain entry")
		}
		name, _ := entry["name"].(string)
		if name == "" {
			return false, fmt.Errorf("xdns: domain name is empty")
		}
		for key, defaultValue := range map[string]string{"lenLimit": "255", "labelLimit": "63", "edns0": "1232"} {
			if value, exists := entry[key]; exists && fmt.Sprint(value) != defaultValue && (key == "edns0" || fmt.Sprint(value) != "0") {
				return false, fmt.Errorf("xdns: custom %s requires a newer core than v26.6.27", key)
			}
		}
		types, _ := entry["types"].([]any)
		if len(types) == 0 {
			types = []any{json.Number("16")}
		}
		for _, recordType := range types {
			method := map[string]string{"1": "a", "28": "aaaa", "16": "txt"}[fmt.Sprint(recordType)]
			if method == "" {
				return false, fmt.Errorf("xdns: record type %v requires a newer core than v26.6.27", recordType)
			}
			specs = append(specs, name+":"+method)
		}
		changed = true
	}
	legacyResolvers := []any{}
	for _, resolver := range resolvers {
		if legacy, ok := resolver.(string); ok {
			legacyResolvers = append(legacyResolvers, legacy)
			continue
		}
		entry, ok := resolver.(map[string]any)
		if !ok || entry["type"] != "udp" {
			return false, fmt.Errorf("xdns: bundled core supports UDP resolvers only")
		}
		options, _ := entry["settings"].(map[string]any)
		addr, _ := options["addr"].(string)
		if addr == "" || len(specs) == 0 {
			return false, fmt.Errorf("xdns: UDP resolver requires an address and domain")
		}
		for _, spec := range specs {
			legacyResolvers = append(legacyResolvers, fmt.Sprint(spec)+"+udp://"+addr)
		}
		changed = true
	}
	if changed {
		settings["domains"] = specs
		if len(resolvers) > 0 {
			settings["resolvers"] = legacyResolvers
		}
	}
	return changed, nil
}
