package session

import (
	"errors"
	"reflect"
	"testing"

	"github.com/ch1y1z1/xiangqi_go/internal/domain"
	"github.com/ch1y1z1/xiangqi_go/internal/engine"
)

func TestBackReturnsBeforeHumanMove(t *testing.T) {
	for _, tc := range []struct {
		name          string
		initial, ai   domain.Side
		depth, target int // -1 means there is no human move to undo.
		paused        bool
	}{
		{"root", domain.Red, domain.Black, 0, -1, false},
		{"AI thinking", domain.Red, domain.Black, 1, 0, false},
		{"AI replied", domain.Red, domain.Black, 2, 0, false},
		{"later human move", domain.Red, domain.Black, 3, 2, false},
		{"later AI reply", domain.Red, domain.Black, 4, 2, false},
		{"AI moved first", domain.Red, domain.Red, 1, -1, false},
		{"human black moved", domain.Red, domain.Red, 2, 1, false},
		{"AI red replied", domain.Red, domain.Red, 3, 1, false},
		{"black starts", domain.Black, domain.Red, 2, 0, false},
		{"paused still uses assigned side", domain.Red, domain.Black, 2, 0, true},
		{"manual", domain.Red, "", 4, 3, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			study := initialStudy()
			study.InitialSide = tc.initial
			ids := []string{study.RootID}
			moves := []string{"a3a4", "a6a5", "c3c4", "c6c5"}
			if tc.initial == domain.Black {
				moves = []string{"a6a5", "a3a4", "c6c5", "c3c4"}
			}
			for _, uci := range moves[:tc.depth] {
				ids = append(ids, study.Play(move(uci)))
			}
			s := New(study, nil, nil)
			s.SetAI(tc.ai)
			if tc.paused {
				s.PauseAI()
			}
			if s.CanBack() != (tc.target >= 0) {
				t.Fatalf("CanBack = %v", s.CanBack())
			}
			s.Back()
			want := study.CurrentID
			if tc.target >= 0 {
				want = ids[tc.target]
			}
			if s.Study.CurrentID != want || s.AISide != tc.ai || s.Paused != tc.paused || !reflect.DeepEqual(s.Study.Nodes, study.Nodes) {
				t.Fatalf("incorrect undo: current=%s want=%s ai=%s paused=%v", s.Study.CurrentID, want, s.AISide, s.Paused)
			}
		})
	}
}

func TestRepeatedBackAndBranches(t *testing.T) {
	study, a, _, deep := branchStudy()
	study.CurrentID = deep
	third := study.Play(move("c3c4"))
	study.Play(move("c6c5"))
	s := New(study, nil, nil)
	s.SetAI(domain.Black)
	s.Back()
	if s.Study.CurrentID != deep {
		t.Fatal("first undo did not undo the latest human turn")
	}
	s.Back()
	if s.Study.CurrentID != study.RootID || s.CanBack() {
		t.Fatal("second undo did not return before the first human turn")
	}
	if !reflect.DeepEqual(s.Study.Nodes, study.Nodes) || s.Study.Node(third) == nil {
		t.Fatal("undo removed a saved branch")
	}
	s.Jump(a) // The other descendants must not influence this line's undo.
	s.Back()
	if s.Study.CurrentID != study.RootID {
		t.Fatal("undo crossed into another branch")
	}
	s.SetAI(domain.Red)
	s.Jump(deep)
	s.Back()
	if s.Study.CurrentID != a {
		t.Fatal("changing roles did not change which side belongs to the human")
	}
}

func TestNavigationRejectsQueuedAutomaticMove(t *testing.T) {
	for _, action := range []string{"back", "root", "forward", "branch"} {
		t.Run(action, func(t *testing.T) {
			study, a, b, deep := branchStudy()
			study.CurrentID = a
			h := newHarness(t, study, false)
			s := h.s
			s.SetAI(domain.Black)
			h.activate(false)
			old := h.next("search")
			result := score(10)
			result.BestMove = "a6a5"
			old.reply <- answer{result: result}
			queued := receive(t, h.ui)
			want := study.RootID
			switch action {
			case "back":
				s.Back()
			case "root":
				s.Root()
			case "forward":
				if !s.Forward() {
					t.Fatal("expected single continuation")
				}
				want = deep
			case "branch":
				s.Jump(b)
				want = b
			}
			queued()
			if s.Study.CurrentID != want || s.Paused || s.AISide != domain.Black || len(s.Study.Nodes) != len(study.Nodes) {
				t.Fatal("navigation changed control state or accepted the old automatic move")
			}
			h.reply(h.next("inspect"), answer{rules: rules()})
			if action == "branch" {
				fresh := h.next("search")
				if !reflect.DeepEqual(fresh.moves, []string{"c3c4"}) {
					t.Fatal("AI did not follow the newly selected branch")
				}
				h.reply(fresh, answer{result: result})
				h.reply(h.next("inspect"), answer{rules: rules()})
			} else {
				h.noJob() // The new position belongs to the human.
			}
		})
	}
}

func TestPausedNavigationAndRoleChangesStayPaused(t *testing.T) {
	study, a, b, deep := branchStudy()
	study.CurrentID = deep
	for _, navigate := range []func(*Session){(*Session).Back, (*Session).Root,
		func(s *Session) { s.Jump(a) }, func(s *Session) { s.Jump(b) },
		func(s *Session) { s.Jump(a); s.Forward() }} {
		h := newHarness(t, study, false)
		s := h.s
		s.SetAI(domain.Black)
		s.PauseAI()
		navigate(s)
		s.SetAI(domain.Black) // Reselecting the current mode is not resume.
		s.SetAI(domain.Red)
		if !s.Paused || s.AISide != domain.Red {
			t.Fatal("navigation or role change implicitly resumed AI")
		}
		h.activate(false)
		h.noJob()
	}
}

func TestNoHumanMoveCannotInterruptAIOpening(t *testing.T) {
	h := newHarness(t, initialStudy(), false)
	s := h.s
	s.SetAI(domain.Red)
	h.activate(false)
	opening := h.next("search")
	s.Back()
	if s.CanBack() || opening.ctx.Err() != nil || s.Paused {
		t.Fatal("undo without a human move interrupted the opening")
	}
	h.reply(opening, answer{result: score(10)})
	h.reply(h.next("inspect"), answer{rules: rules()})
	current := s.Study.CurrentID
	s.Back()
	if s.CanBack() || s.Study.CurrentID != current {
		t.Fatal("undo removed the AI opening")
	}
}

func TestPauseAndHintsDoNotImplicitlyChangeControl(t *testing.T) {
	h := newHarness(t, initialStudy(), false)
	s := h.s
	s.SetAI(domain.Red)
	h.activate(false)
	auto := h.next("search")
	s.Recommend()
	if auto.ctx.Err() != nil || s.Paused || s.CanRecommend() {
		t.Fatal("hint interrupted an automatic turn")
	}
	auto.reply <- answer{result: score(10)}
	queued := receive(t, h.ui)
	s.PauseAI()
	queued()
	if !s.Paused || len(s.Study.History()) != 0 || !s.CanRecommend() {
		t.Fatal("pause accepted a queued automatic move")
	}
	s.Recommend()
	h.reply(h.next("search"), answer{result: score(20)})
	if !s.Paused || s.Suggestion == nil || len(s.Study.History()) != 0 {
		t.Fatal("paused hint automatically moved or resumed AI")
	}
	s.Adopt()
	h.reply(h.next("inspect"), answer{rules: rules()})
	if !s.Paused || len(s.Study.History()) != 1 {
		t.Fatal("adopting a hint resumed AI")
	}
	s.ResumeAI()
	if s.Paused || s.AISide != domain.Red {
		t.Fatal("explicit resume did not retain the AI role")
	}
	h.noJob() // Human black is now to move.
	s.Root()
	h.reply(h.next("inspect"), answer{rules: rules()})
	auto = h.next("search")
	s.Stop() // Escape is an explicit pause on an automatic turn.
	h.reply(auto, answer{result: score(30)})
	if !s.Paused || s.Study.CurrentID != s.Study.RootID {
		t.Fatal("stopping automatic play failed to pause or accepted a late move")
	}
}

func TestStoppingAnalysisKeepsRunningAI(t *testing.T) {
	for _, background := range []bool{false, true} {
		h := newHarness(t, initialStudy(), false)
		s := h.s
		s.SetAI(domain.Black)
		h.activate(background)
		if !background {
			s.Recommend()
		}
		analysis := h.next("search")
		s.Stop()
		h.reply(analysis, answer{result: score(99)})
		if s.Paused || s.AISide != domain.Black || s.Suggestion != nil {
			t.Fatal("stopping analysis paused AI or accepted a late hint")
		}
		h.noJob()
		s.Drag(move("a3a4").From, move("a3a4").To)
		h.reply(h.next("inspect"), answer{rules: rules()})
		reply := h.next("search")
		if s.job.purpose != automaticSearch {
			t.Fatal("AI did not reply after the next human move")
		}
		result := score(10)
		result.BestMove = "a6a5"
		h.reply(reply, answer{result: result})
	}
}

func TestPausingMovesKeepsStudyAnalysis(t *testing.T) {
	for _, kind := range []string{"rules", "score", "hint"} {
		t.Run(kind, func(t *testing.T) {
			h := newHarness(t, initialStudy(), false)
			s := h.s
			s.SetAI(domain.Black)
			s.SetShowEvaluation(kind == "score")
			s.Activate()
			work := h.next("inspect")
			if kind != "rules" {
				h.reply(work, answer{rules: rules()})
				if kind == "hint" {
					s.Recommend()
				}
				work = h.next("search")
			}
			s.PauseAI()
			if !s.Paused || work.ctx.Err() != nil {
				t.Fatal("pausing automatic moves cancelled study analysis")
			}
			h.reply(work, answer{rules: rules(), result: score(25)})
			if len(s.Study.History()) != 0 || !s.Paused {
				t.Fatal("study analysis moved or resumed AI")
			}
			if kind == "hint" && s.Suggestion == nil {
				t.Fatal("pause discarded the requested hint")
			}
			if kind == "score" && s.EvalText(s.Study.RootID) != "红 +0.25" {
				t.Fatal("pause discarded the background score")
			}
			h.noJob()
		})
	}
}

func TestStoppingRulesOffersRetryWithoutChangingControl(t *testing.T) {
	h := newHarness(t, initialStudy(), false)
	s := h.s
	s.SetAI(domain.Black)
	s.SetShowEvaluation(false)
	s.Activate()
	old := h.next("inspect")
	s.Stop()
	h.reply(old, answer{rules: rules()})
	if s.Paused || s.Rules.Error == "" || s.CanRecommend() {
		t.Fatal("stopped rule check was mistaken for a playable position")
	}
	h.noJob()
	s.Reanalyze()
	h.reply(h.next("inspect"), answer{rules: rules()})
	if !s.CanRecommend() || s.Paused || s.Rules.Error != "" {
		t.Fatal("explicit rule retry did not restore manual play")
	}
}

func TestOpeningBranchesDoesNotPauseAutomaticSearch(t *testing.T) {
	study, a, _, _ := branchStudy()
	study.CurrentID = a
	h := newHarness(t, study, false)
	s := h.s
	s.SetAI(domain.Black)
	h.activate(false)
	auto := h.next("search")
	s.OpenBranches(study.RootID)
	if auto.ctx.Err() != nil || s.Paused || s.job.purpose != automaticSearch {
		t.Fatal("branch comparison interrupted automatic play")
	}
	s.CloseBranches()
	if auto.ctx.Err() != nil {
		t.Fatal("closing branch comparison interrupted automatic play")
	}
}

func TestAutomaticFailureKeepsRoleAndNeedsRetry(t *testing.T) {
	for _, failRules := range []bool{false, true} {
		h := newHarness(t, initialStudy(), false)
		s := h.s
		s.SetAI(domain.Red)
		s.SetShowEvaluation(false)
		s.Activate()
		inspection := h.next("inspect")
		if failRules {
			h.reply(inspection, answer{err: errors.New("engine unavailable")})
		} else {
			h.reply(inspection, answer{rules: rules()})
			h.reply(h.next("search"), answer{err: errors.New("engine unavailable")})
		}
		if s.Paused || s.AISide != domain.Red || !s.AIBlocked() {
			t.Fatal("failure changed user pause state or hid the failure")
		}
		s.OpenBranches(s.Study.RootID)
		s.SetShowEvaluation(true)
		s.SetShowEvaluation(false)
		h.noJob()
		s.Reanalyze()
		if s.AIBlocked() {
			t.Fatal("an explicit retry is still presented as blocked")
		}
		if failRules {
			h.reply(h.next("inspect"), answer{rules: rules()})
		}
		h.reply(h.next("search"), answer{result: score(10)})
		h.reply(h.next("inspect"), answer{rules: rules()})
		if s.Paused || s.AIBlocked() || len(s.Study.History()) != 1 {
			t.Fatal("explicit retry did not restore automatic play")
		}
	}
}

func TestRapidNavigationOnlyRunsTheFinalPosition(t *testing.T) {
	study, a, b, _ := branchStudy()
	study.CurrentID = a
	h := newHarness(t, study, true)
	s := h.s
	s.SetAI(domain.Black)
	h.activate(false)
	old := h.next("search")
	s.Jump(b)
	s.Root()
	s.Jump(b)
	for range 3 {
		receive(t, h.f.stops) <- nil
	}
	result := score(10)
	result.BestMove = "a6a5"
	old.reply <- answer{result: result}
	// The old search and the two superseded inspections all enqueue callbacks.
	for range 3 {
		h.dispatch()
	}
	final := h.next("inspect")
	if !reflect.DeepEqual(final.moves, []string{"c3c4"}) || s.Paused || s.Study.CurrentID != b {
		t.Fatal("an intermediate position escaped cancellation")
	}
	h.reply(final, answer{rules: rules()})
	if latest := h.next("search"); !reflect.DeepEqual(latest.moves, []string{"c3c4"}) {
		t.Fatal("automatic search did not use the final position")
	}
}

func TestTerminalNavigationKeepsAutomaticControl(t *testing.T) {
	for _, ai := range []domain.Side{domain.Black, domain.Red} {
		study := initialStudy()
		study.Play(move("a3a4"))
		study.Play(move("a6a5"))
		h := newHarness(t, study, false)
		s := h.s
		s.SetAI(ai)
		s.SetShowEvaluation(false)
		s.Activate()
		h.reply(h.next("inspect"), answer{rules: engine.Snapshot{Finished: true, Winner: "black"}})
		if s.Paused || s.AISide != ai {
			t.Fatal("terminal position changed control settings")
		}
		h.noJob()
		if ai == domain.Black {
			s.Back()
		} else {
			s.Root()
		}
		h.reply(h.next("inspect"), answer{rules: rules()})
		if s.Paused || s.AISide != ai || s.Study.CurrentID != study.RootID {
			t.Fatal("leaving the terminal position changed control settings")
		}
		if ai == domain.Red {
			h.reply(h.next("search"), answer{result: score(10)})
			h.reply(h.next("inspect"), answer{rules: rules()})
		} else {
			h.noJob()
		}
	}
}
