// GPL-3.0-or-later. Adapted from xiangqi_app PikafishBridge.mm (391fce78).
// Protocol, scheduling and Foundation-free desktop adaptation: 2026-10-07.
#include "attacks.h"
#include "engine.h"
#include "movegen.h"
#include "position.h"
#include "score.h"
#include "uci.h"
#include <nlohmann/json.hpp>
#include <condition_variable>
#include <deque>
#include <filesystem>
#include <iostream>
#include <memory>
#include <mutex>
#include <optional>
#include <sstream>
#include <thread>

using namespace Stockfish;
using json = nlohmann::json;
constexpr size_t MaxLine = 1024 * 1024;
constexpr const char* Commit = "4c17cee11f888ae1d48a9494f2e2239f019f0a1f";
constexpr const char* NetworkSHA = "7d13d73569a9b571ba0eb20cf1596247bc2a42738967e61afef6482b231e900e";

static std::optional<PositionSetError> loadPosition(Position& p, std::deque<StateInfo>& states,
                                                  const json& request) {
    states.emplace_back();
    auto error = p.set(request.at("fen").get<std::string>(), &states.back());
    if (error) return error;
    for (const auto& value : request.at("moves")) {
        const auto text = value.get<std::string>();
        Move move = UCIEngine::to_move(p, text);
        if (move == Move::none()) return PositionSetError("Illegal move: " + text);
        states.emplace_back();
        p.do_move(move, states.back());
    }
    return std::nullopt;
}

static json inspect(const json& request) {
    Position p;
    std::deque<StateInfo> states;
    if (auto error = loadPosition(p, states, request)) return {{"error", error->what()}};
    json legal = json::array(), captures = json::array();
    for (Move move : MoveList<LEGAL>(p)) legal.push_back(UCIEngine::move(move));
    const bool check = bool(p.checkers());
    Value value = VALUE_ZERO;
    const bool ruled = p.rule_judge(value);
    const bool finished = ruled || legal.empty();
    std::string winner, outcome;
    if (finished && !(ruled && value == VALUE_DRAW)) {
        Color color = ruled && value > 0 ? p.side_to_move() : ~p.side_to_move();
        winner = color == WHITE ? "red" : "black";
    }
    if (ruled) outcome = value == VALUE_DRAW ? "和棋" : value < 0 ? "当前行棋方判负" : "当前行棋方胜出";
    else if (legal.empty()) outcome = check ? "将死" : "困毙";
    if (request.value("hints", false)) {
        for (Color color : {WHITE, BLACK})
            for (Move move : p.safe_captures(color))
                captures.push_back({{"move", UCIEngine::move(move)}, {"side", color == WHITE ? "red" : "black"}});
    }
    return {{"error", ""}, {"legalMoves", legal}, {"check", check}, {"finished", finished},
            {"winner", winner}, {"outcome", outcome}, {"captures", captures}};
}

class Worker {
    std::ostream& output;
    std::mutex outputMutex, control;
    std::condition_variable wake;
    std::deque<json> inspections;
    std::optional<json> pendingSearch;
    std::unique_ptr<Engine> engine;
    bool quitting = false, busy = false, cancelled = false, configured = false;
    std::string activeID, network;
    std::thread searchThread, rulesThread;

    void respond(const std::string& id, const json& result) {
        emit({{"version", 1}, {"id", id}, {"type", "result"}, {"result", result}});
    }
    void fail(const std::string& id, const std::string& error) {
        emit({{"version", 1}, {"id", id}, {"type", "error"}, {"error", error}});
    }
    void emit(const json& value) {
        std::lock_guard<std::mutex> lock(outputMutex);
        output << value.dump() << '\n' << std::flush;
    }
    json search(const json& request) {
        Position validation;
        std::deque<StateInfo> states;
        if (auto error = loadPosition(validation, states, request)) throw std::runtime_error(error->what());
        {
            std::lock_guard<std::mutex> lock(control);
            if (cancelled) return {{"cancelled", true}};
        }
        if (!engine) {
            // Slow model loading never holds control or the stdin reader.
            auto fresh = std::make_unique<Engine>();
            fresh->get_options().add_info_listener([](auto) {});
            fresh->set_on_verify_network([](auto) {});
            auto option = [&](std::string value) {
                std::istringstream stream(value); fresh->get_options().setoption(stream);
            };
            option("name EvalFile value " + network);
            option("name Hash value 32");
            option("name Threads value 1");
            option("name MultiPV value 1");
            std::lock_guard<std::mutex> lock(control);
            engine = std::move(fresh);
        }
        json result = {{"bestMove", ""}, {"pv", json::array()}, {"depth", 0}, {"score", 0},
                       {"mate", false}, {"bound", ""}, {"hasScore", false}, {"cancelled", false}};
        engine->set_on_update_no_moves([](const auto&) {});
        engine->set_on_iter([](const auto&) {});
        engine->set_on_start([] {});
        // Shared ownership keeps callbacks valid even if a future upstream go() throws.
        auto out = std::make_shared<json>(result);
        engine->set_on_update_full([out](const Engine::InfoFull& info) {
            (*out)["depth"] = info.depth;
            (*out)["bound"] = std::string(info.bound);
            const bool mate = info.score.is<Score::Mate>();
            (*out)["mate"] = mate;
            (*out)["score"] = mate ? info.score.get<Score::Mate>().plies : info.score.get<Score::InternalUnits>().value;
            (*out)["hasScore"] = true;
            std::istringstream stream{std::string(info.pv)};
            json pv = json::array();
            for (std::string move; stream >> move;) pv.push_back(move);
            (*out)["pv"] = pv;
        });
        engine->set_on_bestmove([out](std::string_view move, std::string_view) { (*out)["bestMove"] = std::string(move); });
        if (auto error = engine->set_position(request.at("fen"), request.at("moves").get<std::vector<std::string>>()))
            throw std::runtime_error(error->what());
        Search::LimitsType limits;
        limits.movetime = request.at("milliseconds").get<int>();
        limits.startTime = now();
        {
            std::lock_guard<std::mutex> lock(control);
            if (cancelled) return {{"cancelled", true}};
            engine->go(limits);
        }
        engine->wait_for_search_finished();
        engine->set_on_update_full([](const auto&) {});
        engine->set_on_bestmove([](auto, auto) {});
        return *out;
    }
    void runSearch() {
        for (;;) {
            json request;
            {
                std::unique_lock<std::mutex> lock(control);
                wake.wait(lock, [&] { return quitting || pendingSearch.has_value(); });
                if (!pendingSearch) return;
                request = std::move(*pendingSearch);
                pendingSearch.reset();
            }
            json result;
            std::string error;
            try { result = search(request); }
            catch (const std::exception& e) { error = e.what(); }
            {
                std::lock_guard<std::mutex> lock(control);
                // Publish completion before allowing another search. A targeted stop
                // can never cancel a later request, including during model loading.
                if (cancelled) respond(activeID, {{"cancelled", true}});
                else if (!error.empty()) fail(activeID, error);
                else respond(activeID, result);
                busy = false;
                activeID.clear();
            }
        }
    }
    void runRules() {
        for (;;) {
            json request;
            {
                std::unique_lock<std::mutex> lock(control);
                wake.wait(lock, [&] { return quitting || !inspections.empty(); });
                if (quitting) return;
                request = std::move(inspections.front()); inspections.pop_front();
            }
            try { respond(request.at("id"), inspect(request)); }
            catch (const std::exception& e) { fail(request.at("id"), e.what()); }
        }
    }
public:
    explicit Worker(std::ostream& stream) : output(stream), searchThread([this] { runSearch(); }), rulesThread([this] { runRules(); }) {}
    ~Worker() {
        {
            std::lock_guard<std::mutex> lock(control);
            quitting = true; cancelled = true;
            if (engine) engine->stop();
        }
        wake.notify_all();
        searchThread.join(); rulesThread.join();
    }
    bool accept(const std::string& line) {
        std::string id;
        try {
            auto request = json::parse(line);
            id = request.at("id").get<std::string>();
            if (id.empty() || id.size() > 128) throw std::runtime_error("Invalid request id");
            if (request.at("version") != 1) throw std::runtime_error("Unsupported protocol version");
            const auto op = request.at("op").get<std::string>();
            if (op == "hello") {
                if (configured) throw std::runtime_error("Already configured");
                auto path = request.at("networkPath").get<std::string>();
                if (path.find_first_of("\r\n\0", 0, 3) != std::string::npos || !std::filesystem::is_regular_file(std::filesystem::u8path(path)))
                    throw std::runtime_error("Offline NNUE file missing or invalid path");
                network = path; configured = true;
                respond(id, {{"protocol", 1}, {"engineCommit", Commit}, {"networkSHA256", NetworkSHA},
                             {"capabilities", {"inspect", "safeCaptures", "search", "targetedStop"}}});
            } else if (op == "stop") {
                std::lock_guard<std::mutex> lock(control);
                const std::string target = request.value("target", std::string());
                bool stopped = busy && (target.empty() || target == activeID);
                if (stopped) { cancelled = true; if (engine) engine->stop(); }
                respond(id, {{"stopped", stopped}});
            } else if (op == "shutdown") {
                respond(id, {{"shutdown", true}});
                return false;
            } else if (op == "inspect" || op == "search") {
                if (!configured) throw std::runtime_error("Send hello first");
                auto fen = request.at("fen").get<std::string>();
                auto moves = request.at("moves").get<std::vector<std::string>>();
                if (fen.size() > 512 || moves.size() > 10000) throw std::runtime_error("Position exceeds protocol limits");
                for (const auto& move : moves)
                    if (move.size() != 4 || move[0] < 'a' || move[0] > 'i' || move[2] < 'a' || move[2] > 'i'
                        || move[1] < '0' || move[1] > '9' || move[3] < '0' || move[3] > '9')
                        throw std::runtime_error("Invalid move encoding");
                std::lock_guard<std::mutex> lock(control);
                if (op == "inspect") {
                    if (inspections.size() >= 128) throw std::runtime_error("Inspect queue full");
                    inspections.push_back(request);
                } else {
                    int ms = request.at("milliseconds").get<int>();
                    if (ms < 1 || ms > 600000) throw std::runtime_error("milliseconds must be 1..600000");
                    if (busy) throw std::runtime_error("Search busy; stop and await completion first");
                    busy = true; cancelled = false; activeID = id; pendingSearch = request;
                }
                wake.notify_all();
            } else throw std::runtime_error("Unknown operation");
        } catch (const std::exception& e) { fail(id, e.what()); }
        return true;
    }
};

int main() {
    // Preserve a dedicated protocol stream. All upstream diagnostics go to stderr.
    std::ostream protocol(std::cout.rdbuf());
    std::cout.rdbuf(std::cerr.rdbuf());
    std::cin.tie(nullptr);
    Attacks::init(); Position::init();
    Worker worker(protocol);
    std::string line;
    char ch;
    while (std::cin.get(ch)) {
        if (ch == '\n') {
            if (!line.empty() && !worker.accept(line)) break;
            line.clear();
        } else {
            if (line.size() >= MaxLine) { std::cerr << "JSONL input exceeds 1 MiB\n"; return 2; }
            line.push_back(ch);
        }
    }
    // EOF is an implicit shutdown, including when the parent exits unexpectedly.
    return 0;
}
