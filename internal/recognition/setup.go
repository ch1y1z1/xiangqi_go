package recognition

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/ch1y1z1/xiangqi_go/internal/domain"
)

type RecognizedPiece struct {
	Side   string `json:"side"`
	Kind   string `json:"kind"`
	Column int    `json:"column"`
	Row    int    `json:"row"`
}

type Setup struct {
	Name       string `json:"name"`
	BottomSide string `json:"bottom_side"`
	// Empty means the image did not specify a side; import with red to move.
	SideToMove string            `json:"side_to_move"`
	Notes      string            `json:"notes"`
	Pieces     []RecognizedPiece `json:"pieces"`
}

// UnmarshalJSON preserves Swift's required, non-null integer coordinates.
// Go's ordinary struct decoder otherwise turns missing/null coordinates into 0.
func (p *RecognizedPiece) UnmarshalJSON(data []byte) error {
	var wire struct {
		Side   string `json:"side"`
		Kind   string `json:"kind"`
		Column *int   `json:"column"`
		Row    *int   `json:"row"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	if wire.Column == nil || wire.Row == nil {
		return errors.New("棋子缺少坐标。")
	}
	*p = RecognizedPiece{Side: wire.Side, Kind: wire.Kind, Column: *wire.Column, Row: *wire.Row}
	return nil
}

func (s Setup) Validate() error {
	validSide := func(v string) bool { return v == string(domain.Red) || v == string(domain.Black) }
	if !validSide(s.BottomSide) || (s.SideToMove != "" && !validSide(s.SideToMove)) {
		return errors.New("识别结果的阵营或朝向不正确，请重试。")
	}
	if len(s.Pieces) == 0 {
		return errors.New("没有识别到棋子，请选择包含完整棋盘的图片。")
	}
	squares := map[[2]int]bool{}
	counts := map[[2]string]int{}
	for _, p := range s.Pieces {
		if !validSide(p.Side) {
			return errors.New("识别结果的棋子阵营不正确。")
		}
		known := false
		for _, kind := range domain.Kinds {
			if p.Kind == string(kind) {
				known = true
				break
			}
		}
		if !known {
			return errors.New("识别结果的棋子类型不正确。")
		}
		if p.Column < 0 || p.Column > 8 || p.Row < 0 || p.Row > 9 {
			return errors.New("识别的棋子位置超出棋盘，请重试。")
		}
		square := [2]int{p.Column, p.Row}
		if squares[square] {
			return errors.New("识别结果中有棋子位置重叠，请重试。")
		}
		squares[square] = true
		identity := [2]string{p.Side, p.Kind}
		counts[identity]++
		kind, side := domain.Kind(p.Kind), domain.Side(p.Side)
		if counts[identity] > kind.Limit() {
			return fmt.Errorf("识别出的%s%s数量过多，请重试。", side.Title(), kind.Glyph(side))
		}
	}
	return nil
}

// ChessPieces converts image coordinates, independent of the current UI board.
// Call Validate before using a manually constructed Setup.
func (s Setup) ChessPieces() []domain.Piece {
	pieces := make([]domain.Piece, len(s.Pieces))
	for i, p := range s.Pieces {
		file, rank := p.Column, 9-p.Row
		if s.BottomSide == string(domain.Black) {
			file, rank = 8-p.Column, p.Row
		}
		pieces[i] = domain.Piece{ID: domain.NewID(), Side: domain.Side(p.Side), Kind: domain.Kind(p.Kind), Square: domain.Square{File: file, Rank: rank}}
	}
	return pieces
}

func parseSetup(content string) (Setup, error) {
	var setup Setup
	if err := json.Unmarshal([]byte(content), &setup); err != nil {
		return Setup{}, errors.New("识别结果格式不正确，请重试或换一张更清晰的图片。")
	}
	if err := setup.Validate(); err != nil {
		return Setup{}, err
	}
	return setup, nil
}
