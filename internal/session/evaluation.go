package session

import (
	"fmt"

	"github.com/ch1y1z1/xiangqi_go/internal/domain"
	"github.com/ch1y1z1/xiangqi_go/internal/engine"
)

// Evaluations belong to this session's backend, setup, full node path and budget.
// They are deliberately never persisted as part of a Study.
type evaluation struct {
	key                            string
	score, distance, depth, budget int
	bound                          string
	mate, terminal                 bool
	winner                         domain.Side
}

func scored(result engine.Result, side domain.Side, budget int, key string) (evaluation, bool) {
	if result.Cancelled || !result.HasScore || result.Depth <= 0 {
		return evaluation{}, false
	}
	bound := result.Bound
	if bound == "exact" {
		bound = ""
	}
	if bound != "" && bound != "lowerbound" && bound != "upperbound" {
		return evaluation{}, false
	}
	v := evaluation{key: key, score: result.Score, depth: result.Depth, budget: budget, bound: bound, mate: result.Mate}
	if side == domain.Black {
		v.score = -v.score
		if bound == "lowerbound" {
			v.bound = "upperbound"
		}
		if bound == "upperbound" {
			v.bound = "lowerbound"
		}
	}
	if result.Mate {
		// The bridge supplies signed plies, not UCI's mate-in-moves.
		if result.Score > 0 {
			v.distance = result.Score/2 + result.Score%2
		} else {
			v.distance = -(result.Score / 2)
		}
		v.winner = domain.Black
		if v.score > 0 {
			v.winner = domain.Red
		}
	}
	return v, true
}

func (v evaluation) text() string {
	if v.terminal {
		if v.winner == domain.Red {
			return "红胜"
		}
		if v.winner == domain.Black {
			return "黑胜"
		}
		return "和棋"
	}
	if v.mate {
		color, approximate := "黑", ""
		if v.winner == domain.Red {
			color = "红"
		}
		if v.bound != "" {
			approximate = "≈"
		}
		return fmt.Sprintf("%s %sM%d", color, approximate, v.distance)
	}
	symbol := ""
	if v.bound == "lowerbound" {
		symbol = "≥"
	}
	if v.bound == "upperbound" {
		symbol = "≤"
	}
	return fmt.Sprintf("红 %s%+.2f", symbol, float64(v.score)/100)
}

func (v evaluation) detail() string {
	if v.terminal {
		return "已结束 · 规则判定"
	}
	detail := fmt.Sprintf("深度 %d · %.1f 秒分析", v.depth, float64(v.budget)/1000)
	if v.mate && v.bound != "" {
		detail += " · 初步胜线"
	}
	return detail
}
