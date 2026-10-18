package service

import (
	"encoding/json"
	"testing"

	"github.com/8bit-warrior/Miniray/internal/database"
	"github.com/8bit-warrior/Miniray/internal/database/model"
)

// Older imports can encode numeric client fields as strings; normalize on import.
func TestAddInboundAcceptsLegacyStringClientFields(t *testing.T) {
	setupConflictDB(t)
	settings := `{"clients":[` +
		`{"id":"11111111-1111-1111-1111-111111111111","email":"legacy-a","limitIp":"","subId":"s-a","enable":true,"totalGB":0,"expiryTime":0,"reset":0},` +
		`{"id":"22222222-2222-2222-2222-222222222222","email":"legacy-b","limitIp":"7","subId":"s-b","enable":true,"totalGB":0,"expiryTime":0,"reset":0}` +
		`],"decryption":"none","fallbacks":[]}`
	in := makeImportInbound("in-9201-tcp", 9201, settings, nil)

	saved, _, err := (&InboundService{}).AddInbound(in)
	if err != nil {
		t.Fatalf("AddInbound: %v", err)
	}

	var rec model.ClientRecord
	if err := database.GetDB().Where("email = ?", "legacy-b").First(&rec).Error; err != nil {
		t.Fatalf("read client: %v", err)
	}
	if rec.LimitIP != 7 {
		t.Fatalf("client limitIp = %d, want 7 parsed from the string", rec.LimitIP)
	}
	var stored struct {
		Clients []map[string]any `json:"clients"`
	}
	if err := json.Unmarshal([]byte(saved.Settings), &stored); err != nil {
		t.Fatalf("stored settings: %v", err)
	}
	for _, c := range stored.Clients {
		if _, isString := c["limitIp"].(string); isString {
			t.Fatalf("stored settings keep a string limitIp for %v: %v", c["email"], c["limitIp"])
		}
	}
}
