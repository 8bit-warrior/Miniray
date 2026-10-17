package service

import (
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/8bit-warrior/Miniray/internal/database"
	"github.com/8bit-warrior/Miniray/internal/database/model"

	"github.com/8bit-warrior/Miniray/internal/web/runtime"
	"github.com/8bit-warrior/Miniray/internal/xray"

	"gorm.io/gorm"
)

func (s *InboundService) runtimeFor(ib *model.Inbound) (runtime.Runtime, error) {
	mgr := runtime.GetManager()
	if mgr == nil {
		return nil, fmt.Errorf("runtime manager not initialised")
	}
	return mgr.Local(), nil
}

func (s *InboundService) localRuntimePlan(ib *model.Inbound) (runtime.Runtime, bool, bool, error) {
	rt, err := s.runtimeFor(ib)
	if err != nil {
		return nil, false, false, nil
	}
	return rt, true, false, nil
}

// Keep idle connections online across several five-second traffic polls.
const onlineGracePeriodMs int64 = 20000

func (s *InboundService) GetOnlineClients() []string {
	process := currentXrayProcess()
	if process == nil {
		return []string{}
	}
	return process.GetOnlineClients()
}

func (s *InboundService) GetClientsLastOnline() (map[string]int64, error) {
	db := database.GetDB()
	var rows []xray.ClientTraffic
	err := db.Model(&xray.ClientTraffic{}).Select("email, last_online").Find(&rows).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	result := make(map[string]int64, len(rows))
	for _, r := range rows {
		result[r.Email] = r.LastOnline
	}
	return result, nil
}

// RefreshLocalOnlineClients refreshes local activity and prunes expired observations.
func (s *InboundService) RefreshLocalOnlineClients(activeEmails, activeInboundTags []string) {
	if process := currentXrayProcess(); process != nil {
		process.RefreshLocalOnline(activeEmails, activeInboundTags, time.Now().UnixMilli(), onlineGracePeriodMs)
	}
}

func (s *InboundService) FilterAndSortClientEmails(emails []string) ([]string, []string, error) {
	db := database.GetDB()

	// Step 1: Get ClientTraffic records for emails in the input list.
	// Chunked to stay under SQLite's bind-variable limit on huge inputs.
	uniqEmails := uniqueNonEmptyStrings(emails)
	clients := make([]xray.ClientTraffic, 0, len(uniqEmails))
	for _, batch := range chunkStrings(uniqEmails, sqliteMaxVars) {
		var page []xray.ClientTraffic
		if err := db.Where("email IN ?", batch).Find(&page).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, err
		}
		clients = append(clients, page...)
	}

	// Step 2: Sort clients by (Up + Down) descending
	sort.Slice(clients, func(i, j int) bool {
		return (clients[i].Up + clients[i].Down) > (clients[j].Up + clients[j].Down)
	})

	// Step 3: Extract sorted valid emails and track found ones
	validEmails := make([]string, 0, len(clients))
	found := make(map[string]bool)
	for _, client := range clients {
		validEmails = append(validEmails, client.Email)
		found[client.Email] = true
	}

	// Step 4: Identify emails that were not found in the database
	extraEmails := make([]string, 0)
	for _, email := range emails {
		if !found[email] {
			extraEmails = append(extraEmails, email)
		}
	}

	return validEmails, extraEmails, nil
}

func (s *InboundService) GetActiveInboundTags() []string {
	if p := currentXrayProcess(); p != nil {
		return p.GetLocalActiveInbounds()
	}
	return []string{}
}
