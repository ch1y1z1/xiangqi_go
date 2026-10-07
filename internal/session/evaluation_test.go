package session

import (
	"testing"

	"github.com/ch1y1z1/xiangqi_go/internal/domain"
	"github.com/ch1y1z1/xiangqi_go/internal/engine"
)

func TestRedPerspectiveBoundsAndMatePlies(t *testing.T) {
	tests := []struct {
		name        string
		side        domain.Side
		score       int
		mate        bool
		bound, want string
	}{
		{"red", domain.Red, 123, false, "", "红 +1.23"},
		{"black", domain.Black, 123, false, "", "红 -1.23"},
		{"red-lower", domain.Red, 23, false, "lowerbound", "红 ≥+0.23"},
		{"black-lower", domain.Black, 23, false, "lowerbound", "红 ≤-0.23"},
		{"black-upper", domain.Black, -23, false, "upperbound", "红 ≥+0.23"},
		{"mate-positive-odd", domain.Red, 5, true, "", "红 M3"},
		{"mate-positive-even", domain.Red, 4, true, "", "红 M2"},
		{"mate-negative-odd", domain.Red, -5, true, "", "黑 M2"},
		{"mate-negative-even", domain.Red, -4, true, "", "黑 M2"},
		{"mate-black", domain.Black, 5, true, "upperbound", "黑 ≈M3"},
		{"mate-black-losing", domain.Black, -5, true, "", "红 M2"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v, ok := scored(engine.Result{HasScore: true, Depth: 8, Score: tt.score, Mate: tt.mate, Bound: tt.bound}, tt.side, 300, "key")
			if !ok || v.text() != tt.want {
				t.Fatalf("got %q valid=%v, want %q", v.text(), ok, tt.want)
			}
			wantDetail := "深度 8 · 0.3 秒分析"
			if tt.mate && tt.bound != "" {
				wantDetail += " · 初步胜线"
			}
			if v.detail() != wantDetail {
				t.Fatal(v.detail())
			}
		})
	}
}

func TestUnusableScoreIsNotZero(t *testing.T) {
	for _, r := range []engine.Result{{Depth: 10}, {HasScore: true}, {HasScore: true, Depth: 10, Cancelled: true}, {HasScore: true, Depth: 10, Bound: "unknown"}} {
		if _, ok := scored(r, domain.Red, 1000, "key"); ok {
			t.Fatal("unusable score accepted")
		}
	}
}
