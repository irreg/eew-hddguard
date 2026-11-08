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
		// The live P2P地震情報 feed sends prefs WITHOUT the 県 suffix
		// (e.g. 東京 rather than 東京都); user-written full names must
		// still match.
		{"feed drops 県 suffix", Event{Prefs: []string{"千葉", "東京", "神奈川"}}, []string{"東京都"}, false, true},
		{"watch without suffix", Event{Prefs: []string{"神奈川"}}, []string{"神奈川"}, false, true},
		{"tokyo normalized", Event{Prefs: []string{"東京"}}, []string{"東京都"}, false, true},
		{"kyoto fu", Event{Prefs: []string{"京都"}}, []string{"京都府"}, false, true},
		{"hokkaido", Event{Prefs: []string{"北海道"}}, []string{"北海道"}, false, true},
		{"normalized mismatch", Event{Prefs: []string{"東京"}}, []string{"埼玉県"}, false, false},
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

func TestNormalizePref(t *testing.T) {
	tests := map[string]string{
		"神奈川県":  "神奈川",
		"神奈川":   "神奈川",
		"東京都":   "東京",
		"京都府":   "京都",
		"京都":    "京都", // must not collapse to 京
		"北海道":   "北海",
		"大阪府":   "大阪",
		" 東京都 ": "東京",
	}
	for in, want := range tests {
		if got := normalizePref(in); got != want {
			t.Errorf("normalizePref(%q) = %q, want %q", in, got, want)
		}
	}
}
