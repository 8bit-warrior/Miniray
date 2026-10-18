package panel

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/8bit-warrior/Miniray/internal/web/service"
)

const maxActionsJSONBytes = 4 << 20

var (
	actionsAPIBase       = "https://api.github.com/repos/8bit-warrior/Miniray"
	actionsClient        = defaultActionsClient
	actionsDigestPattern = regexp.MustCompile("^sha256:[0-9a-f]{64}$")
)

func defaultActionsClient() *http.Client {
	return (&service.SettingService{}).NewProxiedHTTPClient(12 * time.Second)
}

type ActionBuild struct {
	RunID     string `json:"runId"`
	Commit    string `json:"commit"`
	Version   string `json:"version"`
	CreatedAt string `json:"createdAt"`
	Title     string `json:"title"`
}

func actionsJSON(path string, target any) error {
	client := actionsClient()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, actionsAPIBase+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GitHub Actions returned HTTP %d", resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, maxActionsJSONBytes)).Decode(target)
}

func (s *PanelService) ListActionBuilds() ([]ActionBuild, error) {
	var runs struct {
		WorkflowRuns []struct {
			ID           int64  `json:"id"`
			HeadSHA      string `json:"head_sha"`
			HeadBranch   string `json:"head_branch"`
			Status       string `json:"status"`
			Conclusion   string `json:"conclusion"`
			CreatedAt    string `json:"created_at"`
			DisplayTitle string `json:"display_title"`
		} `json:"workflow_runs"`
	}
	if err := actionsJSON("/actions/workflows/release.yml/runs?branch=main&status=success&per_page=100", &runs); err != nil {
		return nil, err
	}

	var artifacts struct {
		Artifacts []struct {
			ID          int64  `json:"id"`
			Name        string `json:"name"`
			Digest      string `json:"digest"`
			Expired     bool   `json:"expired"`
			WorkflowRun struct {
				ID      int64  `json:"id"`
				HeadSHA string `json:"head_sha"`
			} `json:"workflow_run"`
		} `json:"artifacts"`
	}
	artifactPath := "/actions/artifacts?name=miniray-linux-" + runtime.GOARCH + "&per_page=100"
	if err := actionsJSON(artifactPath, &artifacts); err != nil {
		return nil, err
	}
	available := make(map[int64]string, len(artifacts.Artifacts))
	for _, artifact := range artifacts.Artifacts {
		if !artifact.Expired && artifact.ID > 0 && actionsDigestPattern.MatchString(artifact.Digest) &&
			artifact.Name == "miniray-linux-"+runtime.GOARCH {
			available[artifact.WorkflowRun.ID] = strings.ToLower(artifact.WorkflowRun.HeadSHA)
		}
	}
	builds := make([]ActionBuild, 0, len(runs.WorkflowRuns))
	for _, run := range runs.WorkflowRuns {
		if run.ID <= 0 || run.HeadBranch != "main" || run.Status != "completed" || run.Conclusion != "success" ||
			!isCommitSHA(run.HeadSHA) || !strings.EqualFold(available[run.ID], run.HeadSHA) {
			continue
		}
		version := "dev+" + shortCommit(run.HeadSHA)
		builds = append(builds, ActionBuild{
			RunID:     strconv.FormatInt(run.ID, 10),
			Commit:    shortCommit(run.HeadSHA),
			Version:   version,
			CreatedAt: run.CreatedAt,
			Title:     run.DisplayTitle,
		})
	}
	sort.Slice(builds, func(i, j int) bool {
		if builds[i].CreatedAt != builds[j].CreatedAt {
			return builds[i].CreatedAt > builds[j].CreatedAt
		}
		a, _ := strconv.ParseInt(builds[i].RunID, 10, 64)
		b, _ := strconv.ParseInt(builds[j].RunID, 10, 64)
		return a > b
	})
	return builds, nil
}

func (s *PanelService) StartUpdateBuild(runID string) (int64, error) {
	id, err := strconv.ParseInt(runID, 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("invalid Actions run ID")
	}
	builds, err := s.ListActionBuilds()
	if err != nil {
		return 0, err
	}
	for _, build := range builds {
		if build.RunID == runID {
			return s.startUpdate(false, runID)
		}
	}
	return 0, fmt.Errorf("Actions build is unavailable or has not passed all checks")
}
