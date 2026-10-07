// GPL-3.0. App-specific extension to the pinned Pikafish Position.
#include "position.h"
#include "attacks.h"
#include <algorithm>

namespace Stockfish {
using namespace Attacks;

// Only called on the bridge's disposable rule Position, never the search Position.
std::vector<Move> Position::safe_captures(Color color) {
    std::vector<Move> result;
    const Color original = sideToMove;
    sideToMove = color;
    std::fill(std::begin(idBoard), std::end(idBoard), 0);
    Bitboard attackers = pieces(color);
    while (attackers) {
        Square from = pop_lsb(attackers);
        PieceType type = type_of(piece_on(from));
        Bitboard targets = (type == PAWN ? attacks_bb<PAWN>(from, color)
                                         : attacks_bb(type, from, pieces()))
                         & pieces(~color) & ~pieces(KING);
        while (targets) {
            Square to = pop_lsb(targets);
            Move move(from, to);
            // Unlike Position::legal, this checks king safety without relying on turn caches.
            if (!chase_legal(move)) continue;
            const auto [captured, id] = do_move(move);
            bool canRecapture = false;
            Bitboard defenders = attackers_to(to) & pieces(sideToMove);
            while (defenders) {
                if (chase_legal(Move(pop_lsb(defenders), to))) {
                    canRecapture = true;
                    break;
                }
            }
            undo_move(move, captured, id);
            if (!canRecapture) result.push_back(move);
        }
    }
    sideToMove = original;
    return result;
}
}
