package domain

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"
)

// Date is Foundation Date's default JSON representation: seconds since
// 2001-01-01T00:00:00Z, including fractional seconds. It is not Unix time.
type Date float64

const referenceUnixSeconds = 978307200

func NewDate(t time.Time) Date {
	return Date(float64(t.Unix()-referenceUnixSeconds) + float64(t.Nanosecond())/1e9)
}
func (d Date) Time() time.Time {
	seconds, fraction := math.Modf(float64(d))
	return time.Unix(int64(seconds)+referenceUnixSeconds, int64(math.Round(fraction*1e9))).UTC()
}
func (d *Date) UnmarshalJSON(data []byte) error {
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return fmt.Errorf("Swift 日期必须是数值")
	}
	var seconds float64
	if err := json.Unmarshal(data, &seconds); err != nil {
		return err
	}
	*d = Date(seconds)
	return nil
}

type Node struct {
	ID       string   `json:"id"`
	ParentID string   `json:"parentID,omitempty"`
	Move     *Move    `json:"move,omitempty"`
	Children []string `json:"children"`
}

func (n Node) MarshalJSON() ([]byte, error) {
	type wire Node
	if n.Children == nil {
		n.Children = []string{}
	}
	return json.Marshal(wire(n))
}

type Study struct {
	ID            string  `json:"id"`
	Name          string  `json:"name"`
	CreatedAt     Date    `json:"createdAt"`
	ModifiedAt    Date    `json:"modifiedAt"`
	InitialPieces []Piece `json:"initialPieces"`
	InitialSide   Side    `json:"initialSide"`
	Nodes         []Node  `json:"nodes"`
	RootID        string  `json:"rootID"`
	CurrentID     string  `json:"currentID"`
	BottomSide    Side    `json:"bottomSide"`
	IsDraft       bool    `json:"isDraft"`
	SchemaVersion int     `json:"schemaVersion"`
}

func (s Study) MarshalJSON() ([]byte, error) {
	type wire Study
	if s.InitialPieces == nil {
		s.InitialPieces = []Piece{}
	}
	if s.Nodes == nil {
		s.Nodes = []Node{}
	}
	return json.Marshal(wire(s))
}

func NewStudy(name string, pieces []Piece, side Side) Study {
	if side == "" {
		side = Red
	}
	root := Node{ID: NewID(), Children: []string{}}
	now := NewDate(time.Now())
	return Study{ID: NewID(), Name: name, CreatedAt: now, ModifiedAt: now,
		InitialPieces: append([]Piece{}, pieces...), InitialSide: side, Nodes: []Node{root},
		RootID: root.ID, CurrentID: root.ID, BottomSide: Red, IsDraft: true, SchemaVersion: 1}
}

// Clone is a deep copy suitable for handing to another goroutine or an editor.
func (s Study) Clone() Study {
	s.InitialPieces = append([]Piece{}, s.InitialPieces...)
	s.Nodes = cloneNodes(s.Nodes)
	return s
}

func cloneNodes(nodes []Node) []Node {
	result := make([]Node, len(nodes))
	for i, node := range nodes {
		result[i] = node
		result[i].Children = append([]string{}, node.Children...)
		if node.Move != nil {
			move := *node.Move
			result[i].Move = &move
		}
	}
	return result
}

// Node returns a pointer into the study. It is invalidated by Play; callers that
// need a snapshot should use Clone. UUID comparisons are case insensitive.
func (s Study) Node(id string) *Node {
	for i := range s.Nodes {
		if strings.EqualFold(s.Nodes[i].ID, id) {
			return &s.Nodes[i]
		}
	}
	return nil
}

// Line returns the complete root-to-node path, excluding the root. Broken or
// cyclic paths return no line instead of hanging on a damaged document.
func (s Study) Line(id string) []Node {
	line := make([]Node, 0)
	seen := make(map[string]bool)
	cursor := s.Node(id)
	for cursor != nil && cursor.ParentID != "" {
		key := strings.ToUpper(cursor.ID)
		if seen[key] {
			return nil
		}
		seen[key] = true
		line = append(line, *cursor)
		cursor = s.Node(cursor.ParentID)
	}
	if cursor == nil || !strings.EqualFold(cursor.ID, s.RootID) {
		return nil
	}
	for i, j := 0, len(line)-1; i < j; i, j = i+1, j-1 {
		line[i], line[j] = line[j], line[i]
	}
	return cloneNodes(line)
}

func (s Study) piecesAt(id string) []Piece {
	pieces := append([]Piece{}, s.InitialPieces...)
	for _, node := range s.Line(id) {
		if node.Move != nil {
			pieces = Applying(*node.Move, pieces)
		}
	}
	return pieces
}
func (s Study) CurrentPieces() []Piece { return s.piecesAt(s.CurrentID) }
func (s Study) SideToMove() Side {
	if len(s.Line(s.CurrentID))%2 == 0 {
		return s.InitialSide
	}
	return s.InitialSide.Opponent()
}
func (s Study) InitialFEN() string { return FEN(s.InitialPieces, s.InitialSide) }

// History returns UCI moves on the entire selected path, for engine repetition
// and rule adjudication. Equal board positions on different paths stay distinct.
func (s Study) History() []string {
	result := make([]string, 0)
	for _, node := range s.Line(s.CurrentID) {
		if node.Move != nil {
			result = append(result, node.Move.UCI())
		}
	}
	return result
}

// Play assumes the engine has checked legality. It reuses matching children only
// under the current parent. An invalid current node leaves the study unchanged.
func (s *Study) Play(move Move) string {
	if s.Node(s.CurrentID) == nil {
		return ""
	}
	s.Nodes = cloneNodes(s.Nodes)
	parent := s.Node(s.CurrentID)
	for _, childID := range parent.Children {
		child := s.Node(childID)
		if child != nil && child.Move != nil && *child.Move == move {
			s.CurrentID = child.ID
			s.ModifiedAt = NewDate(time.Now())
			return child.ID
		}
	}
	child := Node{ID: NewID(), ParentID: parent.ID, Move: &move, Children: []string{}}
	parent.Children = append(parent.Children, child.ID)
	s.Nodes = append(s.Nodes, child)
	s.CurrentID = child.ID
	s.ModifiedAt = NewDate(time.Now())
	return child.ID
}

func (s Study) Label(node Node) string {
	if node.Move == nil || node.ParentID == "" {
		return "起点"
	}
	return Notation(*node.Move, s.piecesAt(node.ParentID))
}
func (s Study) BranchCount() int {
	count := 0
	for _, n := range s.Nodes {
		if len(n.Children) > 1 {
			count++
		}
	}
	return count
}
func (s Study) Duplicate() Study {
	copy := s.Clone()
	copy.ID = NewID()
	copy.Name += " · 副本"
	copy.CreatedAt = NewDate(time.Now())
	copy.ModifiedAt = copy.CreatedAt
	return copy
}
func (s Study) EditSetup(name string, pieces []Piece, side, bottom Side) Study {
	edited := s.Clone()
	edited.Name = strings.TrimSpace(name)
	if edited.Name == "" {
		edited.Name = "未命名残局"
	}
	edited.BottomSide = bottom
	edited.ModifiedAt = NewDate(time.Now())
	if s.InitialFEN() != FEN(pieces, side) {
		edited.InitialPieces = append([]Piece{}, pieces...)
		edited.InitialSide = side
		root := Node{ID: NewID(), Children: []string{}}
		edited.Nodes = []Node{root}
		edited.RootID = root.ID
		edited.CurrentID = root.ID
	}
	// The caller sets IsDraft from engine validation; invalid setups remain savable.
	return edited
}

func Examples() []Study {
	fens := []string{"3k5/9/4R4/9/9/9/9/9/9/4K4 w - - 0 1", "4k4/9/9/9/4p4/9/3N5/9/R8/4K4 w - - 0 1", InitialFEN}
	names := []string{"单车研究", "车马练习", "从开局开始"}
	result := make([]Study, len(fens))
	for i, fen := range fens {
		pieces, _ := PiecesFromFEN(fen)
		result[i] = NewStudy(names[i], pieces, Red)
		result[i].IsDraft = false
	}
	return result
}
