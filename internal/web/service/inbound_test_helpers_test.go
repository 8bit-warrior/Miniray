package service

import (
	"testing"

	"github.com/8bit-warrior/Miniray/internal/database"
	"github.com/8bit-warrior/Miniray/internal/database/model"
	"github.com/8bit-warrior/Miniray/internal/xray"
)

func seedClientTraffic(t *testing.T, inboundId int, email string, enable bool) {
	t.Helper()
	row := xray.ClientTraffic{InboundId: inboundId, Email: email, Enable: enable}
	if err := database.GetDB().Create(&row).Error; err != nil {
		t.Fatalf("seed traffic %s: %v", email, err)
	}
}

func loadInboundByTag(t *testing.T, tag string) *model.Inbound {
	t.Helper()
	var ib model.Inbound
	if err := database.GetDB().Where("tag = ?", tag).First(&ib).Error; err != nil {
		t.Fatalf("load inbound %s: %v", tag, err)
	}
	return &ib
}
