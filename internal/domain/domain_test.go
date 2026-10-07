package domain

import (
	"encoding/json"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

func move(t *testing.T, uci string) Move {
	t.Helper()
	m, err := ParseMove(uci)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestSwiftJSONAndDates(t *testing.T) {
	data, err := os.ReadFile("testdata/swift-study.json")
	if err != nil {
		t.Fatal(err)
	}
	var s Study
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatal(err)
	}
	if s.CreatedAt != -978307200 || !s.CreatedAt.Time().Equal(time.Unix(0, 0)) || s.ModifiedAt != 812345678.125 {
		t.Fatalf("wrong Swift dates: %v %v", s.CreatedAt, s.ModifiedAt)
	}
	if s.BottomSide != Black || !s.IsDraft || s.SchemaVersion != 1 || s.BranchCount() != 1 || !slices.Equal(s.History(), []string{"e7d7"}) {
		t.Fatalf("lost Swift fields: %+v", s)
	}
	encoded, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	var before, after any
	if err := json.Unmarshal(data, &before); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(encoded, &after); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("Swift JSON changed:\n%s", encoded)
	}
	for _, date := range []time.Time{time.Unix(0, 0), time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC), time.Unix(-1, 750000000), time.Now()} {
		if delta := NewDate(date).Time().Sub(date); delta < -time.Microsecond || delta > time.Microsecond {
			t.Fatalf("date precision lost: %s", delta)
		}
	}
	for _, invalid := range []string{`null`, `"2001-01-01T00:00:00Z"`, `true`} {
		var d Date
		if json.Unmarshal([]byte(invalid), &d) == nil {
			t.Errorf("accepted nonnumeric date %s", invalid)
		}
	}
	if _, err := json.Marshal(Date(math.NaN())); err == nil {
		t.Error("accepted nonfinite date")
	}
	empty, err := json.Marshal(NewStudy("草稿", nil, Red))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(empty), `"initialPieces":[]`) || !strings.Contains(string(empty), `"children":[]`) || strings.Contains(string(empty), `"parentID"`) || strings.Contains(string(empty), `"move"`) {
		t.Fatalf("wrong empty/optional encoding: %s", empty)
	}
}

// Set XIANGQI_SWIFT_DOMAIN to the original App/Domain.swift to run the actual
// Swift -> Go -> Swift interoperability probe. Regular tests use its fixture.
func TestSwiftInterop(t *testing.T) {
	sourcePath := os.Getenv("XIANGQI_SWIFT_DOMAIN")
	if sourcePath == "" {
		t.Skip("optional original Swift interoperability probe")
	}
	source, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile("testdata/swift-study.json")
	if err != nil {
		t.Fatal(err)
	}
	var s Study
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatal(err)
	}
	newID := s.Play(move(t, "d9e9"))
	if newID == "" {
		t.Fatal("failed to play")
	}
	dir := t.TempDir()
	document := filepath.Join(dir, "roundtrip.json")
	data, err = json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(document, data, 0600); err != nil {
		t.Fatal(err)
	}
	probe := `
let data = try Data(contentsOf: URL(fileURLWithPath: CommandLine.arguments[1]))
let study = try JSONDecoder().decode(Study.self, from: data)
precondition(study.nodes.count == 4 && study.branchCount == 1)
precondition(study.currentLine.compactMap { $0.move?.uci } == ["e7d7", "d9e9"])
precondition(study.createdAt.timeIntervalSince1970 == 0)
precondition(study.bottomSide == .black && study.isDraft && study.schemaVersion == 1)
let encoder = JSONEncoder()
FileHandle.standardOutput.write(try encoder.encode(study))
`
	script := filepath.Join(dir, "main.swift")
	if err := os.WriteFile(script, append(source, []byte(probe)...), 0600); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command("swift", script, document).CombinedOutput()
	if err != nil {
		t.Fatalf("Swift decode failed: %v\n%s", err, output)
	}
	var returned Study
	if err := json.Unmarshal(output, &returned); err != nil {
		t.Fatalf("Swift encode failed: %v\n%s", err, output)
	}
	if !reflect.DeepEqual(s, returned) {
		t.Fatal("Swift round trip changed study")
	}
}

func TestHistoryBranchesAndStableIDs(t *testing.T) {
	s := NewStudy("研究", InitialPieces(), Red)
	initial := s.Clone()
	first := s.Play(move(t, "a0a1"))
	if s.SideToMove() != Black || s.CurrentPieces()[16].ID == "" {
		t.Fatal("lost turn or identity")
	}
	s.CurrentID = s.RootID
	if id := s.Play(move(t, "a0a1")); id != first || len(s.Nodes) != 2 {
		t.Fatal("same-parent move did not reuse child")
	}
	s.CurrentID = s.RootID
	second := s.Play(move(t, "a0a2"))
	if second == first || s.BranchCount() != 1 || !slices.Equal(s.Node(s.RootID).Children, []string{first, second}) {
		t.Fatal("branch order/reuse broken")
	}
	if !reflect.DeepEqual(initial.Nodes, []Node{{ID: s.RootID, Children: []string{}}}) {
		t.Fatal("Play mutated a prior snapshot")
	}
	// Two complete move cycles reach the same board as the root, but retain
	// distinct nodes/history for repetition adjudication.
	s = NewStudy("历史", InitialPieces(), Red)
	for _, uci := range []string{"a0a1", "a9a8", "a1a0", "a8a9"} {
		s.Play(move(t, uci))
	}
	if FEN(s.CurrentPieces(), s.SideToMove()) != s.InitialFEN() || s.CurrentID == s.RootID || len(s.History()) != 4 {
		t.Fatal("history was merged by board position")
	}
	if s.Node(strings.ToLower(s.CurrentID)) == nil {
		t.Fatal("UUID case not accepted")
	}
	if s.Label(*s.Node(s.RootID)) != "起点" {
		t.Fatal("wrong root label")
	}
	copy := s.Duplicate()
	if copy.ID == s.ID || copy.RootID != s.RootID || copy.InitialPieces[0].ID != s.InitialPieces[0].ID || copy.Name != "历史 · 副本" {
		t.Fatal("duplicate identity incorrect")
	}
	copy.Nodes[1].Move.To = Square{8, 8}
	copy.Nodes[0].Children[0] = "changed"
	copy.InitialPieces[0].ID = "changed"
	if s.Nodes[1].Move.To == (Square{8, 8}) || s.Nodes[0].Children[0] == "changed" || s.InitialPieces[0].ID == "changed" {
		t.Fatal("duplicate aliases original")
	}
	pieces := InitialPieces() // Same setup, deliberately regenerated piece IDs.
	edited := s.EditSetup("  改名  ", pieces, Red, Black)
	if edited.Name != "改名" || edited.RootID != s.RootID || edited.CurrentID != s.CurrentID || edited.InitialPieces[0].ID != s.InitialPieces[0].ID || edited.BottomSide != Black {
		t.Fatal("rename/flip/same-FEN edit lost history or IDs")
	}
	edited = s.EditSetup(" \n ", nil, Black, Red)
	if edited.Name != "未命名残局" || len(edited.Nodes) != 1 || edited.RootID == s.RootID || edited.CurrentID != edited.RootID || edited.InitialSide != Black || !edited.IsDraft {
		t.Fatal("setup edit did not reset root")
	}
	s.Nodes[0].ParentID = s.CurrentID
	if s.Line(s.CurrentID) != nil {
		t.Fatal("cyclic path should be rejected")
	}
}

func TestNotationAndFEN(t *testing.T) {
	if FEN(InitialPieces(), Red) != InitialFEN {
		t.Fatal("initial FEN does not round trip")
	}
	for _, fen := range []string{"", "9/9", "9/9/9/9/9/9/9/9/9/8", "9/9/9/9/9/9/9/9/9/91", "9/9/9/9/9/9/9/9/9/8x"} {
		if _, err := PiecesFromFEN(fen); err == nil {
			t.Errorf("accepted bad FEN %q", fen)
		}
	}
	for _, uci := range []string{"a0a0", "j0a0", "a0a:", "A0a1", "a0a10"} {
		if _, err := ParseMove(uci); err == nil {
			t.Errorf("accepted bad move %q", uci)
		}
	}
	for _, tc := range []struct{ uci, want string }{{"h2e2", "炮二平五"}, {"h0g2", "马二进三"}, {"h9g7", "马8进7"}, {"a9a8", "车1进1"}, {"a0a1", "车九进一"}} {
		if got := Notation(move(t, tc.uci), InitialPieces()); got != tc.want {
			t.Errorf("%s: got %s want %s", tc.uci, got, tc.want)
		}
	}
	for _, side := range []Side{Red, Black} {
		pieces := []Piece{{"1", side, Pawn, Square{4, 2}}, {"2", side, Pawn, Square{4, 4}}, {"3", side, Pawn, Square{4, 6}}}
		want := "前兵进一"
		m := Move{Square{4, 6}, Square{4, 7}}
		if side == Black {
			want = "前卒进1"
			m = Move{Square{4, 2}, Square{4, 1}}
		}
		if got := Notation(m, pieces); got != want {
			t.Fatalf("peer notation: %s", got)
		}
		m = Move{Square{4, 4}, Square{5, 4}}
		want = "中兵平四"
		if side == Black {
			want = "中卒平6"
		}
		if got := Notation(m, pieces); got != want {
			t.Fatalf("middle peer notation: %s", got)
		}
	}
}

func TestEditorInventoryAndArrayHistory(t *testing.T) {
	e := NewEditor(nil)
	for i := 0; i < 2; i++ {
		if err := e.Tap(Square{i, 0}); err != nil {
			t.Fatal(err)
		}
	}
	if e.PlacementKind != "" {
		t.Fatal("placement remained on after stock exhausted")
	}
	e.Choose(Rook, Red)
	if err := e.Tap(Square{2, 0}); err == nil || len(e.Pieces) != 2 {
		t.Fatal("inventory overflow accepted")
	}
	id := e.Pieces[0].ID
	if err := e.Move(Square{0, 0}, Square{1, 0}); err == nil || len(e.Pieces) != 2 || *e.Selected != (Square{0, 0}) {
		t.Fatal("editor allowed capture")
	}
	if err := e.Tap(Square{0, 1}); err != nil {
		t.Fatal(err)
	}
	if e.Pieces[0].ID != id || e.Selected != nil {
		t.Fatal("move changed ID/selection")
	}
	e.Choose(Pawn, Black)
	e.Undo()
	if e.Pieces[0].Square != (Square{0, 0}) || e.PlacementSide != Black || e.PlacementKind != "" || !e.CanRedo() {
		t.Fatal("undo restored more than piece array")
	}
	e.Redo()
	if e.Pieces[0].Square != (Square{0, 1}) || e.Pieces[0].ID != id {
		t.Fatal("redo lost piece identity")
	}
	if err := e.Tap(Square{0, 1}); err != nil {
		t.Fatal(err)
	}
	e.DeleteSelected()
	if len(e.Pieces) != 1 || e.Selected != nil || e.PlacementKind != "" {
		t.Fatal("delete left a selection or delete mode")
	}
	e.Undo()
	e.Replace(nil)
	if e.CanRedo() || len(e.Pieces) != 0 {
		t.Fatal("new edit retained redo")
	}
	e.Undo()
	if len(e.Pieces) != 2 {
		t.Fatal("clear not undoable")
	}
	external := InitialPieces()
	e.Replace(external)
	external[0].ID = "changed"
	if e.Pieces[0].ID == "changed" {
		t.Fatal("replace aliases caller array")
	}
	for _, side := range []Side{Red, Black} {
		for _, kind := range Kinds {
			board := NewEditor(nil)
			board.Choose(kind, side)
			for i := 0; i < kind.Limit(); i++ {
				if err := board.Tap(Square{i, 0}); err != nil {
					t.Fatal(err)
				}
			}
			board.Choose(kind, side)
			if err := board.Tap(Square{8, 9}); err == nil {
				t.Fatalf("%s %s exceeded inventory", side, kind)
			}
		}
	}
}
