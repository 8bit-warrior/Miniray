package paneloffline

import (
	"testing"
	"time"
)

func TestOfflineDuration(t *testing.T) {
	cases := []struct {
		enabled  bool
		n        int
		unit     string
		valid    bool
		duration time.Duration
	}{
		{false, 0, "", true, 0},
		{true, 1, "minute", true, time.Minute},
		{true, 525600, "minute", true, 365 * 24 * time.Hour},
		{true, 8760, "hour", true, 365 * 24 * time.Hour},
		{true, 365, "day", true, 365 * 24 * time.Hour},
		{true, 0, "minute", false, 0},
		{true, -2, "hour", false, 0},
		{true, 366, "day", false, 0},
		{true, 8761, "hour", false, 0},
		{true, 525601, "minute", false, 0},
		{true, 99999999, "hour", false, 0},
		{true, 1, "week", false, 0},
	}
	for _, c := range cases {
		d, e := Duration(c.enabled, c.n, c.unit)
		if (e == nil) != c.valid || (c.valid && d != c.duration) {
			t.Errorf("%+v gives %v,%v", c, d, e)
		}
	}
}
