package panel

import (
	"encoding/json"
	"testing"
)

func TestPanelUpdateInfoSeparatesStableReleaseFromDevelopmentBuild(t *testing.T) {
	info := PanelUpdateInfo{
		Channel:             "development",
		CurrentVersion:      "0.0.6_9356f1e",
		LatestVersion:       "v0.0.7_abcd123",
		StableLatestVersion: "v0.0.6",
		UpdateAvailable:     true,
	}
	encoded, err := json.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Channel             string `json:"channel"`
		LatestVersion       string `json:"latestVersion"`
		StableLatestVersion string `json:"stableLatestVersion"`
		UpdateAvailable     bool   `json:"updateAvailable"`
	}
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Channel != "development" || decoded.LatestVersion != "v0.0.7_abcd123" ||
		decoded.StableLatestVersion != "v0.0.6" || !decoded.UpdateAvailable {
		t.Fatalf("unexpected panel update response: %+v", decoded)
	}
}
