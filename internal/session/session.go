// Package session owns the UI-thread study controller and its asynchronous engine
// scheduler. Call every public method and read every public field on the UI
// thread; mutate state through methods, not exported fields. Set Dispatch before
// Activate; Dispatch must enqueue on that same
// thread, and must remain callable from workers after Leave (it may discard
// callbacks after the window closes). Workers never access mutable UI state.
package session

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/ch1y1z1/xiangqi_go/internal/domain"
	"github.com/ch1y1z1/xiangqi_go/internal/engine"
)

// Backend must allow Stop concurrently with Inspect/Search, and honor context
// cancellation. A session borrows an exclusive backend; its owner closes it.
type Backend interface {
	Inspect(context.Context, string, []string, bool) (engine.Snapshot, error)
	Search(context.Context, string, []string, int) (engine.Result, error)
	Stop() error
}

type purpose uint8

const (
	rulesTask purpose = iota
	branchInspect
	evaluationSearch
	recommendSearch
	automaticSearch
)

type request struct {
	purpose                         purpose
	generation                      uint64
	currentKey, targetKey, targetID string
	budget                          int
	position                        domain.Study
	ctx                             context.Context
	cancel                          context.CancelFunc
}

type evaluationFailure struct{ key, message string }

type Session struct {
	Study                                        domain.Study
	Rules                                        engine.Snapshot
	Selected                                     *domain.Square
	Suggestion                                   *engine.Result
	AISide                                       domain.Side // Empty means both sides are manual.
	Paused, Busy, RulesBusy, Active, Saved       bool
	Error                                        string
	Budget                                       int // Milliseconds; UI presets are 300, 1000 and 3000.
	ShowLights, ShowEvaluation, ShowBranchScores bool
	BranchParentID                               string
	AnalyzingID                                  string // Background target, including its rule inspection.
	Dispatch                                     func(func())
	OnChange                                     func()

	backend                       Backend
	persist                       func(domain.Study) error
	generation                    uint64
	job                           *request
	tail                          <-chan struct{} // Backend completion AND all preceding Stop completions.
	rulesReady, rulesHints        bool
	halted, pendingRecommendation bool
	preferred                     map[string]string
	evaluations                   map[string]evaluation
	failures                      map[string]evaluationFailure
}

// New performs no engine I/O. Nil persistence is useful for unsaved previews.
func New(study domain.Study, backend Backend, persist func(domain.Study) error) *Session {
	done := make(chan struct{})
	close(done)
	s := &Session{Study: cloneStudy(study), backend: backend, persist: persist,
		Saved: true, Budget: 1000, ShowLights: true, ShowEvaluation: true, ShowBranchScores: true,
		tail: done, preferred: make(map[string]string), evaluations: make(map[string]evaluation), failures: make(map[string]evaluationFailure)}
	s.rememberLine(study.CurrentID)
	return s
}

func cloneStudy(study domain.Study) domain.Study {
	study.InitialPieces = slices.Clone(study.InitialPieces)
	study.Nodes = slices.Clone(study.Nodes)
	for i := range study.Nodes {
		study.Nodes[i].Children = slices.Clone(study.Nodes[i].Children)
		if study.Nodes[i].Move != nil {
			m := *study.Nodes[i].Move
			study.Nodes[i].Move = &m
		}
	}
	return study
}

func (s *Session) changed() {
	if s.OnChange != nil {
		s.OnChange()
	}
}

func (s *Session) Activate() {
	if s.Active {
		return
	}
	if s.Dispatch == nil || s.backend == nil {
		s.Error = "无法启动研究：需要配置 UI Dispatch 和引擎 Backend。"
		s.changed()
		return
	}
	s.Active, s.halted = true, false
	s.pump()
	s.changed()
}

func (s *Session) Leave() {
	s.Active = false
	s.invalidate()
	if s.AISide != "" {
		s.Paused = true
	}
	s.halted = true
	s.save()
	s.changed()
}

func (s *Session) controlled() bool {
	return s.AISide != "" && s.AISide == s.Study.SideToMove() && !s.Paused
}
func (s *Session) playable() bool {
	return s.Active && s.rulesReady && !s.RulesBusy && s.Rules.Error == "" && !s.Rules.Finished
}

func (s *Session) Tap(square domain.Square) {
	if !square.Valid() || !s.playable() || s.controlled() {
		return
	}
	if s.Selected != nil && *s.Selected != square {
		move := domain.Move{From: *s.Selected, To: square}
		if slices.Contains(s.Rules.LegalMoves, move.UCI()) {
			s.play(move)
			return
		}
	}
	for _, p := range s.Study.CurrentPieces() {
		if p.Square == square && p.Side == s.Study.SideToMove() {
			if s.Selected != nil && *s.Selected == square {
				s.Selected = nil
			} else {
				s.Selected = &square
			}
			s.changed()
			return
		}
	}
	s.Selected = nil
	s.changed()
}

func (s *Session) Drag(from, to domain.Square) {
	if !s.playable() || s.controlled() {
		return
	}
	s.play(domain.Move{From: from, To: to})
}

func (s *Session) play(move domain.Move) {
	if !s.playable() || !slices.Contains(s.Rules.LegalMoves, move.UCI()) {
		return
	}
	s.invalidate()
	s.BranchParentID = ""
	s.Study.Play(move)
	s.rememberLine(s.Study.CurrentID)
	s.refresh()
	s.save()
	s.pump()
	s.changed()
}

func (s *Session) Jump(id string) {
	if s.Study.Node(id) == nil {
		return
	}
	s.invalidate()
	if s.AISide != "" {
		s.Paused = true
	}
	s.BranchParentID = ""
	s.rememberLine(id)
	s.Study.CurrentID = id
	s.refresh()
	s.save()
	s.pump()
	s.changed()
}

func (s *Session) Back() {
	if n := s.Study.Node(s.Study.CurrentID); n != nil && n.ParentID != "" {
		s.Jump(n.ParentID)
	}
}
func (s *Session) Root() { s.Jump(s.Study.RootID) }

func (s *Session) Forward() bool {
	n := s.Study.Node(s.Study.CurrentID)
	if n == nil {
		return false
	}
	if next := s.preferred[n.ID]; next != "" && slices.Contains(n.Children, next) && s.Study.Node(next) != nil {
		s.Jump(next)
		return true
	}
	if len(n.Children) == 1 && s.Study.Node(n.Children[0]) != nil {
		s.Jump(n.Children[0])
		return true
	}
	return false
}

func (s *Session) Flip() {
	s.Study.BottomSide = s.Study.BottomSide.Opponent()
	s.Selected = nil
	s.save()
	s.changed()
}

func (s *Session) SetAI(side domain.Side) {
	if side != "" && side != domain.Red && side != domain.Black {
		return
	}
	s.invalidate()
	s.AISide, s.Paused, s.halted = side, false, false
	s.Error = ""
	s.pump()
	s.changed()
}

func (s *Session) ResumeAI() {
	if s.AISide == "" {
		return
	}
	s.invalidate()
	s.Paused, s.halted = false, false
	s.Error = ""
	s.pump()
	s.changed()
}

func (s *Session) Recommend() {
	if !s.Active {
		s.Activate()
	}
	if !s.Active {
		return
	}
	s.invalidate()
	// An explicit recommendation never becomes an automatic move afterwards.
	if s.AISide != "" {
		s.Paused = true
	}
	s.halted, s.pendingRecommendation = false, true
	s.Error = ""
	s.pump()
	s.changed()
}

func (s *Session) Adopt() {
	if s.Suggestion == nil || !s.playable() {
		return
	}
	move, err := domain.ParseMove(s.Suggestion.BestMove)
	if err == nil {
		s.play(move)
	}
}

func (s *Session) Stop() {
	s.invalidate()
	if s.AISide != "" {
		s.Paused = true
	}
	s.halted = true
	s.changed()
}

func (s *Session) SetBudget(milliseconds int) {
	if milliseconds <= 0 || milliseconds == s.Budget {
		return
	}
	s.invalidate()
	s.Budget = milliseconds
	clear(s.evaluations)
	clear(s.failures)
	s.halted = false
	s.pump()
	s.changed()
}

func (s *Session) SetShowLights(show bool) {
	if show == s.ShowLights {
		return
	}
	s.ShowLights = show
	if !show {
		s.Rules.Captures = nil
	}
	if s.job != nil && (s.job.purpose == rulesTask || s.backgroundJob()) {
		s.cancelJob()
	}
	s.pump()
	s.changed()
}

func (s *Session) SetShowEvaluation(show bool) {
	s.ShowEvaluation = show
	s.reconcileBackground()
	s.pump()
	s.changed()
}

func (s *Session) SetShowBranchScores(show bool) {
	s.ShowBranchScores = show
	s.reconcileBackground()
	s.pump()
	s.changed()
}

func (s *Session) OpenBranches(parentID string) {
	if s.Study.Node(parentID) == nil {
		return
	}
	if s.controlled() || (s.job != nil && s.job.purpose == automaticSearch) {
		s.invalidate()
		s.Paused = true
	}
	s.BranchParentID = parentID
	s.halted = false
	s.reconcileBackground()
	s.pump()
	s.changed()
}

func (s *Session) CloseBranches() {
	s.BranchParentID = ""
	s.reconcileBackground()
	s.pump()
	s.changed()
}

// Reanalyze explicitly retries the current position and, if open, its visible
// branch group. Failed results otherwise remain failed across navigation.
func (s *Session) Reanalyze() {
	s.invalidate()
	delete(s.evaluations, s.Study.CurrentID)
	delete(s.failures, s.Study.CurrentID)
	if n := s.Study.Node(s.BranchParentID); n != nil {
		for _, id := range n.Children {
			delete(s.evaluations, id)
			delete(s.failures, id)
		}
	}
	s.halted = false
	s.Error = ""
	if s.Rules.Error != "" {
		s.rulesReady = false
	}
	s.pump()
	s.changed()
}

func (s *Session) ApplyEdit(edited domain.Study) error {
	s.invalidate()
	if s.AISide != "" {
		s.Paused = true
	}
	copy := cloneStudy(edited)
	if s.persist != nil {
		if err := s.persist(cloneStudy(copy)); err != nil {
			s.Error = "保存失败：" + err.Error()
			s.Saved = false
			s.halted = true
			s.changed()
			return err
		}
	}
	if copy.ID != s.Study.ID || copy.RootID != s.Study.RootID || copy.InitialSide != s.Study.InitialSide || !reflect.DeepEqual(copy.InitialPieces, s.Study.InitialPieces) {
		clear(s.preferred)
		clear(s.evaluations)
		clear(s.failures)
	}
	s.Study = copy
	s.BranchParentID = ""
	s.Saved = true
	s.rememberLine(copy.CurrentID)
	s.refresh()
	s.pump()
	s.changed()
	return nil
}

func (s *Session) EvalText(id string) string {
	if v, ok := s.evaluation(id); ok {
		return v.text()
	}
	if f, ok := s.failures[id]; ok && f.key == s.cacheKey(id) {
		return "未获评分"
	}
	if s.job != nil && s.job.targetID == id {
		return "计算中…"
	}
	return "待评估"
}

func (s *Session) EvalDetail(id string) string {
	if v, ok := s.evaluation(id); ok {
		return v.detail()
	}
	if f, ok := s.failures[id]; ok && f.key == s.cacheKey(id) {
		return f.message
	}
	return s.EvalText(id)
}

func (s *Session) Comparison(id, parentID string) string {
	n := s.Study.Node(parentID)
	if n == nil || len(n.Children) < 2 || !slices.Contains(n.Children, id) {
		return ""
	}
	position := s.Study
	position.CurrentID = parentID
	red := position.SideToMove() == domain.Red
	best, mine := 0, 0
	for i, child := range n.Children {
		v, ok := s.evaluation(child)
		if !ok || v.mate || v.terminal || v.bound != "" {
			return ""
		}
		if i == 0 || (red && v.score > best) || (!red && v.score < best) {
			best = v.score
		}
		if child == id {
			mine = v.score
		}
	}
	if best == mine {
		return "本组较优"
	}
	difference := float64(best)/100 - float64(mine)/100
	if difference < 0 {
		difference = -difference
	}
	return fmt.Sprintf("较本组较优着差 %.2f", difference)
}

func (s *Session) evaluation(id string) (evaluation, bool) {
	v, ok := s.evaluations[id]
	return v, ok && s.Study.Node(id) != nil && v.key == s.cacheKey(id)
}

func (s *Session) positionKey(id string) string {
	var key strings.Builder
	for _, part := range []string{s.Study.ID, s.Study.RootID, s.Study.InitialFEN(), id} {
		key.WriteString(part)
		key.WriteByte(0)
	}
	for _, node := range s.Study.Line(id) {
		key.WriteString(node.ID)
		key.WriteByte(0)
		if node.Move != nil {
			key.WriteString(node.Move.UCI())
			key.WriteByte(0)
		}
	}
	return key.String()
}

func (s *Session) cacheKey(id string) string {
	return fmt.Sprintf("%s\x00%d\x00%s\x00%s", s.positionKey(id), s.Budget, engine.EngineCommit, engine.NetworkSHA256)
}

func (s *Session) rememberLine(id string) {
	for _, n := range s.Study.Line(id) {
		if n.ParentID != "" {
			s.preferred[n.ParentID] = n.ID
		}
	}
}

func (s *Session) save() {
	s.Study.ModifiedAt = domain.NewDate(time.Now())
	if s.persist != nil {
		if err := s.persist(cloneStudy(s.Study)); err != nil {
			s.Saved = false
			s.Error = "保存失败：" + err.Error()
			return
		}
	}
	s.Saved = true
}

func (s *Session) refresh() {
	s.Rules = engine.Snapshot{}
	s.rulesReady = false
	s.Selected = nil
	s.Error = ""
	s.halted = false
}

func (s *Session) invalidate() {
	s.cancelJob()
	s.Suggestion = nil
	s.Selected = nil
	s.pendingRecommendation = false
}

func (s *Session) backgroundJob() bool {
	return s.job != nil && (s.job.purpose == branchInspect || s.job.purpose == evaluationSearch)
}

func (s *Session) wanted(id string) bool {
	if s.ShowEvaluation && id == s.Study.CurrentID {
		return true
	}
	if s.ShowBranchScores {
		if n := s.Study.Node(s.BranchParentID); n != nil {
			return slices.Contains(n.Children, id)
		}
	}
	return false
}

func (s *Session) reconcileBackground() {
	if !s.backgroundJob() {
		return
	}
	current := s.Study.CurrentID
	_, currentScored := s.evaluation(current)
	failure, failed := s.failures[current]
	currentPending := s.ShowEvaluation && !currentScored && !(failed && failure.key == s.cacheKey(current))
	if !s.wanted(s.job.targetID) || (s.job.targetID != current && currentPending) {
		s.cancelJob()
	}
}

// cancelJob never waits on the UI thread. Stop bypasses the blocked backend
// request, but the next backend request must wait for BOTH to finish.
func (s *Session) cancelJob() {
	s.generation++
	if s.job == nil {
		return
	}
	r := s.job
	r.cancel()
	s.job = nil
	s.Busy = false
	s.RulesBusy = false
	s.AnalyzingID = ""
	previous, backend, dispatch := s.tail, s.backend, s.Dispatch
	generation, currentKey := s.generation, s.positionKey(s.Study.CurrentID)
	barrier := make(chan struct{})
	s.tail = barrier
	go func() {
		err := backend.Stop()
		<-previous
		close(barrier)
		if err != nil {
			dispatch(func() {
				if !s.Active || s.generation != generation || s.positionKey(s.Study.CurrentID) != currentKey {
					return
				}
				s.Error = "停止引擎失败：" + err.Error()
				s.changed()
			})
		}
	}()
}

func (s *Session) newRequest(kind purpose, id string) *request {
	ctx, cancel := context.WithCancel(context.Background())
	position := cloneStudy(s.Study)
	position.CurrentID = id
	r := &request{purpose: kind, generation: s.generation, currentKey: s.positionKey(s.Study.CurrentID),
		targetKey: s.cacheKey(id), targetID: id, budget: s.Budget, position: position, ctx: ctx, cancel: cancel}
	s.job = r
	s.RulesBusy = kind == rulesTask
	s.Busy = kind != rulesTask
	if kind == branchInspect || kind == evaluationSearch {
		s.AnalyzingID = id
	} else {
		s.AnalyzingID = ""
	}
	return r
}

func (s *Session) valid(r *request) bool {
	return s.Active && s.job == r && s.generation == r.generation && s.positionKey(s.Study.CurrentID) == r.currentKey && s.Study.Node(r.targetID) != nil && s.cacheKey(r.targetID) == r.targetKey
}

func (s *Session) finish(r *request) {
	r.cancel()
	s.job = nil
	s.RulesBusy = false
	s.Busy = false
	s.AnalyzingID = ""
}

// launch captures all mutable inputs before starting a worker. Its completion
// is only interpreted on the UI thread, even if Dispatch queues it for later.
func (s *Session) inspect(kind purpose, id string, hints bool) {
	r := s.newRequest(kind, id)
	previous, backend, dispatch := s.tail, s.backend, s.Dispatch
	done := make(chan struct{})
	s.tail = done
	fen, moves := r.position.InitialFEN(), slices.Clone(r.position.History())
	go func() {
		<-previous
		var result engine.Snapshot
		err := r.ctx.Err()
		if err == nil {
			result, err = backend.Inspect(r.ctx, fen, moves, hints)
		}
		close(done)
		dispatch(func() {
			if !s.valid(r) {
				return
			}
			s.finish(r)
			if err != nil {
				result.Error = err.Error()
			}
			if kind == rulesTask {
				s.Rules = result
				s.rulesReady = true
				s.rulesHints = hints
				if result.Error != "" {
					s.Error = "规则检查失败：" + result.Error
					s.failures[id] = evaluationFailure{r.targetKey, s.Error + "；请重新分析。"}
					if s.AISide != "" {
						s.Paused = true
					}
					s.pendingRecommendation = false
				}
				s.pump()
			} else if result.Error != "" {
				s.failures[id] = evaluationFailure{r.targetKey, "规则检查失败：" + result.Error + "；请重新分析。"}
				s.pump()
			} else if result.Finished {
				s.terminal(id, result)
				s.pump()
			} else {
				s.search(evaluationSearch, id)
			}
			s.changed()
		})
	}()
}

func (s *Session) search(kind purpose, id string) {
	r := s.newRequest(kind, id)
	previous, backend, dispatch := s.tail, s.backend, s.Dispatch
	done := make(chan struct{})
	s.tail = done
	fen, moves := r.position.InitialFEN(), slices.Clone(r.position.History())
	go func() {
		<-previous
		var result engine.Result
		err := r.ctx.Err()
		if err == nil {
			result, err = backend.Search(r.ctx, fen, moves, r.budget)
		}
		close(done)
		dispatch(func() {
			if !s.valid(r) {
				return
			}
			s.finish(r)
			if err == nil && result.Cancelled {
				err = errors.New("分析已中断，请重试")
			}
			if kind == evaluationSearch {
				if v, ok := scored(result, r.position.SideToMove(), r.budget, r.targetKey); err == nil && ok {
					s.evaluations[id] = v
					delete(s.failures, id)
				} else {
					message := "本次分析未获得评分，请重新分析。"
					if err != nil {
						message = "分析失败：" + err.Error() + "；请重新分析。"
					}
					s.failures[id] = evaluationFailure{r.targetKey, message}
				}
				s.pump()
				s.changed()
				return
			}
			move, parseErr := domain.ParseMove(result.BestMove)
			if err == nil && (parseErr != nil || !slices.Contains(s.Rules.LegalMoves, result.BestMove)) {
				err = errors.New("引擎没有返回当前局面的合法建议，请重试")
			}
			if err != nil {
				s.Error = "AI 分析失败：" + err.Error()
				s.failures[id] = evaluationFailure{r.targetKey, s.Error}
				if s.AISide != "" {
					s.Paused = true
				}
				s.halted = true
				s.changed()
				return
			}
			if v, ok := scored(result, r.position.SideToMove(), r.budget, r.targetKey); ok {
				s.evaluations[id] = v
				delete(s.failures, id)
			} else {
				s.failures[id] = evaluationFailure{r.targetKey, "建议暂无有效评分；可重新分析。"}
			}
			if kind == automaticSearch && s.controlled() {
				s.play(move)
			} else {
				result.PV = slices.Clone(result.PV)
				s.Suggestion = &result
				s.pump()
				s.changed()
			}
		})
	}()
}

func (s *Session) terminal(id string, snapshot engine.Snapshot) {
	s.evaluations[id] = evaluation{key: s.cacheKey(id), budget: s.Budget, terminal: true, winner: domain.Side(snapshot.Winner)}
	delete(s.failures, id)
}

func (s *Session) pump() {
	if !s.Active || s.job != nil || s.halted {
		return
	}
	if s.rulesReady && s.Rules.Error != "" {
		return
	}
	if !s.rulesReady || s.rulesHints != s.ShowLights {
		s.inspect(rulesTask, s.Study.CurrentID, s.ShowLights)
		return
	}
	if s.Rules.Error != "" {
		return
	}
	if s.Rules.Finished {
		s.terminal(s.Study.CurrentID, s.Rules)
		s.pendingRecommendation = false
	}
	if !s.Rules.Finished {
		if s.pendingRecommendation {
			s.pendingRecommendation = false
			s.search(recommendSearch, s.Study.CurrentID)
			return
		}
		if s.controlled() {
			s.search(automaticSearch, s.Study.CurrentID)
			return
		}
	}
	var ids []string
	if s.ShowEvaluation {
		ids = append(ids, s.Study.CurrentID)
	}
	if s.ShowBranchScores {
		if n := s.Study.Node(s.BranchParentID); n != nil {
			ids = append(ids, n.Children...)
		}
	}
	for _, id := range ids {
		if s.Study.Node(id) == nil {
			continue
		}
		if _, ok := s.evaluation(id); ok {
			continue
		}
		if f, ok := s.failures[id]; ok && f.key == s.cacheKey(id) {
			continue
		}
		if id == s.Study.CurrentID {
			s.search(evaluationSearch, id)
		} else {
			s.inspect(branchInspect, id, false)
		}
		return
	}
}
