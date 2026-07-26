package eew

import "testing"

func TestShouldTrigger(t *testing.T) {
	tests := []struct {
		name       string
		ev         Event
		watchPrefs []string
		allowTest  bool
		want       bool
	}{
		{"cancelled", Event{Cancelled: true, Prefs: []string{"東京都"}}, nil, false, false},
		{"test rejected", Event{Test: true, Prefs: []string{"東京都"}}, nil, false, false},
		{"test allowed", Event{Test: true, Prefs: []string{"東京都"}}, nil, true, true},
		{"no filter matches anything", Event{Prefs: []string{"石川県"}}, nil, false, true},
		{"pref match", Event{Prefs: []string{"神奈川県", "東京都"}}, []string{"東京都"}, false, true},
		{"pref mismatch", Event{Prefs: []string{"石川県"}}, []string{"東京都"}, false, false},
		{"empty areas with filter", Event{}, []string{"東京都"}, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, reason := ShouldTrigger(tt.ev, tt.watchPrefs, tt.allowTest)
			if got != tt.want {
				t.Errorf("ShouldTrigger() = %v (%s), want %v", got, reason, tt.want)
			}
			if reason == "" {
				t.Error("reason must not be empty")
			}
		})
	}
}
