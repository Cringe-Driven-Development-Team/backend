package onlineconf

import (
	"encoding/json"
	"errors"
	"strconv"
	"testing"
	"time"
)

// TestRolloutTransitions проверяет выдержку и переходы с управляемым временем.
func TestRolloutTransitions(t *testing.T) {
	start := time.Date(2030, 1, 1, 15, 0, 0, 0, time.FixedZone("MSK", 3*3600))
	now := start.Add(time.Minute)
	flag := func(enabled bool, percent int, since *time.Time) keyRecord {
		return keyRecord{RolloutStartedAt: since, Data: map[string]json.RawMessage{
			"enabled":         json.RawMessage(strconv.FormatBool(enabled)),
			"rollout_percent": json.RawMessage(strconv.Itoa(percent)),
		}}
	}
	cases := []struct {
		name     string
		from, to keyRecord
		at       time.Time
		want     *time.Time
		denied   bool
	}{
		{"first step", flag(false, 0, nil), flag(true, 10, nil), now, &now, false},
		{"skip first step", flag(true, 0, nil), flag(true, 25, nil), now, nil, true},
		{"disabled increase", flag(false, 0, nil), flag(false, 10, nil), now, nil, true},
		{"59 seconds", flag(true, 10, &start), flag(true, 25, nil), now.Add(-time.Second), nil, true},
		{"60 seconds", flag(true, 10, &start), flag(true, 25, nil), now, &now, false},
		{"new hold", flag(true, 25, &now), flag(true, 50, nil), now, nil, true},
		{"decrease", flag(true, 50, &start), flag(true, 10, nil), now, &now, false},
		{"invalid decrease", flag(true, 50, &start), flag(true, 15, nil), now, nil, true},
		{"disable", flag(true, 50, &start), flag(false, 50, nil), now, nil, false},
		{"reenable", flag(false, 50, nil), flag(true, 50, nil), now, &now, false},
		{"reenable and increase", flag(false, 50, &start), flag(true, 100, nil), now, nil, true},
		{"same percent", flag(true, 25, &start), flag(true, 25, nil), now, &start, false},
		{"legacy percent", flag(true, 17, &start), flag(true, 25, nil), now, &now, false},
		{"unknown start", flag(true, 17, nil), flag(true, 25, nil), now, nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := rolloutStart(tc.from, tc.to, tc.at)
			if tc.denied {
				if !errors.Is(err, errRolloutStep) {
					t.Fatalf("expected rollout conflict, got %v", err)
				}
				return
			}
			if err != nil || (got == nil) != (tc.want == nil) || (got != nil && !got.Equal(*tc.want)) {
				t.Fatalf("start = %v, %v; want %v", got, err, tc.want)
			}
		})
	}
	policy := flag(true, 17, &start).policy()
	if policy.NextPercent == nil || *policy.NextPercent != 25 || policy.NextAllowedAt == nil ||
		policy.NextAllowedAt.Format(time.RFC3339) != "2030-01-01T12:01:00Z" {
		t.Fatalf("legacy policy: %+v", policy)
	}
}
