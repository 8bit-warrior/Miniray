package sub

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/8bit-warrior/Miniray/internal/database"
	"github.com/8bit-warrior/Miniray/internal/database/dbtest"
	"github.com/8bit-warrior/Miniray/internal/database/model"
)

func seedSubDB(t *testing.T) {
	t.Helper()
	dbDir := t.TempDir()
	t.Setenv("MINIRAY_DB_FOLDER", dbDir)
	dbtest.InitDB(t, filepath.Join(dbDir, "miniray.db"))
}

func seedSubInbound(t *testing.T, subId, tag string, port, subSortIndex int, stream string) *model.Inbound {
	t.Helper()
	db := database.GetDB()
	uuid := "11111111-2222-4333-8444-" + fmt.Sprintf("%012d", port)
	email := tag + "@e"
	settings := fmt.Sprintf(`{"clients":[{"id":%q,"email":%q,"subId":%q,"enable":true}],"decryption":"none"}`, uuid, email, subId)
	ib := &model.Inbound{
		UserId: 1, Tag: tag, Enable: true, Listen: "203.0.113.5", Port: port,
		Protocol: model.VLESS, Remark: tag, Settings: settings, StreamSettings: stream,
		SubSortIndex: subSortIndex,
	}
	if err := db.Create(ib).Error; err != nil {
		t.Fatalf("seed inbound %s: %v", tag, err)
	}
	client := &model.ClientRecord{Email: email, SubID: subId, UUID: uuid, Enable: true}
	if err := db.Create(client).Error; err != nil {
		t.Fatalf("seed client %s: %v", email, err)
	}
	if err := db.Create(&model.ClientInbound{ClientId: client.Id, InboundId: ib.Id}).Error; err != nil {
		t.Fatalf("seed client_inbound %s: %v", email, err)
	}
	return ib
}

const wsTLSStream = `{"network":"ws","security":"tls","wsSettings":{"path":"/base","host":"base.host"},"tlsSettings":{"serverName":"base.sni"}}`
