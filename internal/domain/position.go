package domain

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

const InitialFEN = "rnbakabnr/9/1c5c1/p1p1p1p1p/9/9/P1P1P1P1P/1C5C1/9/RNBAKABNR w - - 0 1"

func InitialPieces() []Piece { pieces, _ := PiecesFromFEN(InitialFEN); return pieces }

// PiecesFromFEN checks board syntax, not chess legality or inventory limits.
// A board-only FEN is accepted; side/counters are not part of the returned pieces.
func PiecesFromFEN(fen string) ([]Piece, error) {
	fields := strings.Fields(fen)
	if len(fields) == 0 {
		return nil, fmt.Errorf("FEN 不能为空")
	}
	rows := strings.Split(fields[0], "/")
	if len(rows) != 10 {
		return nil, fmt.Errorf("FEN 必须有十行")
	}
	pieces := make([]Piece, 0, 32)
	for row, content := range rows {
		file := 0
		for _, ch := range content {
			if ch >= '1' && ch <= '9' {
				file += int(ch - '0')
			} else {
				var kind Kind
				switch strings.ToLower(string(ch)) {
				case "k":
					kind = King
				case "a":
					kind = Advisor
				case "b":
					kind = Elephant
				case "n":
					kind = Horse
				case "r":
					kind = Rook
				case "c":
					kind = Cannon
				case "p":
					kind = Pawn
				default:
					return nil, fmt.Errorf("FEN 含未知棋子：%c", ch)
				}
				side := Black
				if ch >= 'A' && ch <= 'Z' {
					side = Red
				}
				pieces = append(pieces, Piece{NewID(), side, kind, Square{file, 9 - row}})
				file++
			}
			if file > 9 {
				return nil, fmt.Errorf("FEN 第 %d 行超过九列", row+1)
			}
		}
		if file != 9 {
			return nil, fmt.Errorf("FEN 第 %d 行不足九列", row+1)
		}
	}
	return pieces, nil
}

func FEN(pieces []Piece, side Side) string {
	var board [10][9]string
	for _, p := range pieces {
		if !p.Square.Valid() || board[p.Square.Rank][p.Square.File] != "" {
			continue
		}
		ch := p.Kind.fen()
		if p.Side == Red {
			ch = strings.ToUpper(ch)
		}
		board[p.Square.Rank][p.Square.File] = ch
	}
	var out strings.Builder
	for rank := 9; rank >= 0; rank-- {
		if rank < 9 {
			out.WriteByte('/')
		}
		empty := 0
		for file := 0; file < 9; file++ {
			if ch := board[rank][file]; ch != "" {
				if empty > 0 {
					out.WriteString(strconv.Itoa(empty))
					empty = 0
				}
				out.WriteString(ch)
			} else {
				empty++
			}
		}
		if empty > 0 {
			out.WriteString(strconv.Itoa(empty))
		}
	}
	if side == Red {
		out.WriteString(" w - - 0 1")
	} else {
		out.WriteString(" b - - 0 1")
	}
	return out.String()
}

func Notation(move Move, pieces []Piece) string {
	var piece Piece
	found := false
	for _, p := range pieces {
		if p.Square == move.From {
			piece = p
			found = true
			break
		}
	}
	if !found {
		return move.UCI()
	}
	chinese := [...]string{"一", "二", "三", "四", "五", "六", "七", "八", "九"}
	number := func(n int) string {
		if piece.Side == Red {
			return chinese[max(0, min(8, n-1))]
		}
		return strconv.Itoa(n)
	}
	fileName := func(file int) string {
		if piece.Side == Red {
			return number(9 - file)
		}
		return number(file + 1)
	}
	peers := make([]Piece, 0)
	for _, p := range pieces {
		if p.Side == piece.Side && p.Kind == piece.Kind && p.Square.File == move.From.File {
			peers = append(peers, p)
		}
	}
	sort.SliceStable(peers, func(i, j int) bool {
		if piece.Side == Red {
			return peers[i].Square.Rank > peers[j].Square.Rank
		}
		return peers[i].Square.Rank < peers[j].Square.Rank
	})
	name := piece.Kind.Glyph(piece.Side) + fileName(move.From.File)
	if len(peers) > 1 {
		for i, peer := range peers {
			if peer.ID != piece.ID {
				continue
			}
			prefix := number(i + 1)
			switch {
			case i == 0:
				prefix = "前"
			case i == len(peers)-1:
				prefix = "后"
			case len(peers) == 3:
				prefix = "中"
			}
			name = prefix + piece.Kind.Glyph(piece.Side)
			break
		}
	}
	if move.To.Rank == move.From.Rank {
		return name + "平" + fileName(move.To.File)
	}
	forward := move.To.Rank > move.From.Rank
	if piece.Side == Black {
		forward = !forward
	}
	distance := move.To.Rank - move.From.Rank
	if distance < 0 {
		distance = -distance
	}
	target := number(distance)
	if piece.Kind == Horse || piece.Kind == Elephant || piece.Kind == Advisor {
		target = fileName(move.To.File)
	}
	if forward {
		return name + "进" + target
	}
	return name + "退" + target
}
