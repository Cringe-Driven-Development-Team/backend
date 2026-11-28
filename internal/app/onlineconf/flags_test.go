package onlineconf

import (
	"encoding/json"
	"testing"
)

// TestFlagEnabledFor проверяет порядок правил на сохранённых данных.
func TestFlagEnabledFor(t *testing.T) {
	for _, tt := range []struct {
		name, enabled, percent string
		userID                 int64
		want                   bool
	}{
		{"disabled with inclusion", "false", "100", 1, false},
		{"included at zero", "true", "0", 1, true},
		{"ordinary at zero", "true", "0", 3, false},
		{"excluded at hundred", "true", "100", 2, false},
		{"ordinary at hundred", "true", "100", 3, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			flag := keyRecord{Name: "frontend.example", Type: "flag", Data: map[string]json.RawMessage{
				"enabled": json.RawMessage(tt.enabled), "rollout_percent": json.RawMessage(tt.percent),
				"include_users": json.RawMessage(`[{"id":1,"login":"one"}]`),
				"exclude_users": json.RawMessage(`[{"id":2,"login":"two"}]`),
			}}
			if got, err := flag.flagEnabledFor(tt.userID); err != nil || got != tt.want {
				t.Fatalf("flagEnabledFor(%d) = %t, %v; want %t", tt.userID, got, err, tt.want)
			}
		})
	}
}

// TestFlagRollout проверяет сохранение доли между запросами и копиями записи.
func TestFlagRollout(t *testing.T) {
	evaluate := func(enabled, percent string, userID int64) bool {
		t.Helper()
		var data map[string]json.RawMessage
		if err := json.Unmarshal([]byte(`{"enabled":`+enabled+`,"rollout_percent":`+percent+`,"include_users":[],"exclude_users":[]}`), &data); err != nil {
			t.Fatal(err)
		}
		flag := keyRecord{Name: "frontend.example", Type: "flag", Data: data}
		got, err := flag.flagEnabledFor(userID)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	count25, count50 := 0, 0
	for id := int64(1); id <= 1000; id++ {
		at25 := evaluate("true", "25", id)
		at50 := evaluate("true", "50", id)
		if at25 && !at50 || evaluate("false", "50", id) || evaluate("true", "50", id) != at50 {
			t.Fatalf("unstable rollout for user %d", id)
		}
		if at25 {
			count25++
		}
		if at50 {
			count50++
		}
	}
	if count25 == 0 || count50 <= count25 || count50 == 1000 {
		t.Fatalf("unexpected cohort sizes: %d, %d", count25, count50)
	}
	if evaluate("true", "39", 2) || !evaluate("true", "40", 2) {
		t.Fatal("bucket for frontend.example and user 2 must be 39")
	}
}
