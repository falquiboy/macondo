// Command rackluck decomposes a completed game (from a GCG) into how good each
// player's racks/positions were (opportunity, i.e. luck) versus how much of that
// potential they actually captured (efficiency, i.e. skill). It is meant to
// answer "who played better despite worse tiles?" the way Quackle's luck stat and
// Elise-style leave valuation do, but using macondo's calibrated leaves — in
// particular the Spanish FILE2017 KLV.
//
// Three per-turn quantities, all in equity points (score + leave value):
//
//   Potential  best static-equity play available from the rack in that position.
//              This is the "goodness of the rack" (tiles + board opportunity).
//   Played     equity of the move actually made.
//   Loss       Potential - Played. Inefficiency that turn (skill signal).
//
// And, when -samples > 0, a board-independent draw-luck figure per turn:
//
//   DrawLuck   Potential(actual rack) - mean Potential(counterfactual racks),
//              where each counterfactual keeps the same leave the player held
//              coming into the draw and refills it with tiles drawn at random
//              from the pool the player actually drew from, evaluated on the SAME
//              board. Positive => the tiles fate handed you were better than a
//              random refill would have been.
//
// Caveat: Potential is a *static* valuation. It does not see the defensive or
// pre-endgame value that simulation / the PEG solver would (e.g. a strategic
// pass to stick the opponent with the Q looks like a large Loss here). Read it as
// a first-order, fast luck/efficiency lens, not as a replacement for `analyze`.
//
// Usage:
//
//	MACONDO_DATA_PATH=.../macondo/data go run ./cmd/rackluck game.gcg [-samples 25] [-seed 1] [-lexicon FILE2017]
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/domino14/word-golib/kwg"
	"github.com/domino14/word-golib/tilemapping"

	"github.com/domino14/macondo/config"
	"github.com/domino14/macondo/equity"
	"github.com/domino14/macondo/game"
	"github.com/domino14/macondo/gcgio"
	pb "github.com/domino14/macondo/gen/api/proto/macondo"
	"github.com/domino14/macondo/lexicon"
	"github.com/domino14/macondo/move"
	"github.com/domino14/macondo/movegen"
)

type turnRec struct {
	idx       int
	player    int
	rack      string
	action    string
	potential float64
	played    float64
	loss      float64
	leaveVal  float64
	drawLuck  float64
	hasLuck   bool
}

func main() {
	samples := flag.Int("samples", 100, "counterfactual draws per turn for draw-luck (0 disables)")
	seed := flag.Int64("seed", 1, "RNG seed for counterfactual sampling (deterministic)")
	lexOverride := flag.String("lexicon", "", "override the lexicon named in the GCG (e.g. FILE2017)")
	jsonOut := flag.Bool("json", false, "emit machine-readable JSON instead of the text table")
	quiet := flag.Bool("quiet", true, "suppress macondo's debug/trace logging")
	flag.Parse()
	if *quiet {
		zerolog.SetGlobalLevel(zerolog.WarnLevel)
	}
	_ = log.Logger
	if flag.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "usage: rackluck game.gcg [-samples N] [-seed N] [-lexicon NAME]")
		os.Exit(2)
	}

	cfg := config.DefaultConfig()
	history, err := parseGCGLenient(cfg, flag.Arg(0))
	if err != nil {
		fmt.Fprintf(os.Stderr, "could not parse GCG: %v\n", err)
		os.Exit(1)
	}
	if *lexOverride != "" {
		history.Lexicon = *lexOverride
	}
	lexName := history.Lexicon
	if lexName == "" {
		fmt.Fprintln(os.Stderr, "GCG has no lexicon and none was given with -lexicon")
		os.Exit(1)
	}

	boardLayout, ldName, variant := game.HistoryToVariant(history)
	rules, err := game.NewBasicGameRules(cfg, lexName, boardLayout, ldName, game.CrossScoreAndSet, variant)
	if err != nil {
		fmt.Fprintf(os.Stderr, "could not build rules for %s: %v\n", lexName, err)
		os.Exit(1)
	}
	gd, err := kwg.GetKWG(cfg.WGLConfig(), lexName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "could not load KWG for %s: %v\n", lexName, err)
		os.Exit(1)
	}
	calc, err := equity.NewCombinedStaticCalculator(lexName, cfg, "", equity.PEGAdjustmentFilename)
	if err != nil {
		fmt.Fprintf(os.Stderr, "could not build equity calculator: %v\n", err)
		os.Exit(1)
	}
	if calc.KLV() == nil {
		fmt.Fprintf(os.Stderr, "WARNING: no leave values loaded for %s — the tool needs a KLV (data/strategy/%s/leaves.klv2)\n", lexName, lexName)
	}
	ld := rules.LetterDistribution()
	spanish := lexicon.IsSpanish(lexName)
	rng := rand.New(rand.NewSource(*seed))

	var recs []turnRec
	lastLeave := map[int][]tilemapping.MachineLetter{}
	lastPlayIdx := map[int]int{0: -1, 1: -1}
	var finalScore [2]int

	for i, evt := range history.Events {
		if int(evt.PlayerIndex) < 2 {
			finalScore[evt.PlayerIndex] = int(evt.Cumulative)
		}
		if !analyzable(evt) {
			continue
		}
		p := int(evt.PlayerIndex)

		g, err := game.NewFromHistory(history, rules, i)
		if err != nil {
			fmt.Fprintf(os.Stderr, "replay failed at event %d: %v\n", i, err)
			continue
		}
		g.RecalculateBoard()
		if g.Playing() != pb.PlayState_PLAYING {
			continue
		}
		rack := g.RackFor(p)
		played, err := game.MoveFromEvent(evt, g.Alphabet(), g.Board())
		if err != nil {
			continue
		}

		potential, _ := bestEquity(gd, calc, g, rack, ld, spanish)
		pe := calc.Equity(played, g.Board(), g.Bag(), g.RackFor(1-p))

		rec := turnRec{
			idx:       len(recs) + 1,
			player:    p,
			rack:      rack.String(),
			action:    actionLabel(played),
			potential: potential,
			played:    pe,
			loss:      potential - pe,
			leaveVal:  calc.LeaveValue(played.Leave()),
		}

		if *samples > 0 {
			drawn := multisetDiff(rack.TilesOn(), lastLeave[p])
			sortTiles(drawn)
			if len(drawn) > 0 {
				pool := drawPool(history, rules, g, lastPlayIdx[p], drawn)
				sortTiles(pool)
				if len(pool) >= len(drawn) {
					var sum float64
					kept := append([]tilemapping.MachineLetter(nil), lastLeave[p]...)
					cand := tilemapping.NewRack(g.Alphabet())
					for s := 0; s < *samples; s++ {
						refill := sampleWithout(pool, len(drawn), rng)
						cand.Set(append(append([]tilemapping.MachineLetter(nil), kept...), refill...))
						pot2, _ := bestEquity(gd, calc, g, cand, ld, spanish)
						sum += pot2
					}
					rec.drawLuck = potential - sum/float64(*samples)
					rec.hasLuck = true
				}
			}
		}

		recs = append(recs, rec)
		lastLeave[p] = append([]tilemapping.MachineLetter(nil), played.Leave()...)
		lastPlayIdx[p] = i
	}

	if *jsonOut {
		emitJSON(recs, history.Players, finalScore, *samples > 0)
	} else {
		report(recs, history.Players, finalScore, *samples > 0)
	}
}

// emitJSON prints the per-turn records and per-player aggregates as JSON so the
// orchestrator (analizar-partida) can fuse them with Elise's luck percentiles.
func emitJSON(recs []turnRec, players []*pb.PlayerInfo, finalScore [2]int, luck bool) {
	name := func(i int) string {
		if i < len(players) {
			return players[i].Nickname
		}
		return fmt.Sprintf("P%d", i)
	}
	type jTurn struct {
		Turn      int     `json:"turn"`
		Player    int     `json:"player"`
		Rack      string  `json:"rack"`
		Action    string  `json:"action"`
		Potential float64 `json:"potential"`
		Played    float64 `json:"played"`
		Loss      float64 `json:"loss"`
		LeaveV    float64 `json:"leave_value"`
		DrawLuck  *float64 `json:"draw_luck,omitempty"`
	}
	type jPlayer struct {
		Name     string  `json:"name"`
		Turns    int     `json:"turns"`
		MeanPot  float64 `json:"mean_potential"`
		MeanLoss float64 `json:"mean_loss"`
		TotLuck  float64 `json:"total_draw_luck"`
		Score    int     `json:"score"`
	}
	out := struct {
		HasLuck bool       `json:"has_draw_luck"`
		Players []jPlayer  `json:"players"`
		Turns   []jTurn    `json:"turns"`
	}{HasLuck: luck}
	var sumPot, sumLoss, sumLuck [2]float64
	var n [2]int
	for _, r := range recs {
		jt := jTurn{Turn: r.idx, Player: r.player, Rack: r.rack, Action: r.action,
			Potential: round1(r.potential), Played: round1(r.played), Loss: round1(r.loss), LeaveV: round1(r.leaveVal)}
		if r.hasLuck {
			dl := round1(r.drawLuck)
			jt.DrawLuck = &dl
		}
		out.Turns = append(out.Turns, jt)
		sumPot[r.player] += r.potential
		sumLoss[r.player] += r.loss
		sumLuck[r.player] += r.drawLuck
		n[r.player]++
	}
	for i := 0; i < 2; i++ {
		if n[i] == 0 {
			continue
		}
		out.Players = append(out.Players, jPlayer{
			Name: name(i), Turns: n[i],
			MeanPot: round1(sumPot[i] / float64(n[i])), MeanLoss: round1(sumLoss[i] / float64(n[i])),
			TotLuck: round1(sumLuck[i]), Score: finalScore[i],
		})
	}
	b, _ := json.MarshalIndent(out, "", "  ")
	fmt.Println(string(b))
}

func round1(f float64) float64 { return float64(int(f*10+0.5*sign(f))) / 10 }
func sign(f float64) float64 {
	if f < 0 {
		return -1
	}
	return 1
}

// emptyRackPass matches a GCG move line for a pass made with an empty rack
// (e.g. the final "> Player: - +0 415" MAGPIE emits when a player has run out of
// tiles). macondo's GCG parser rejects the empty rack; such a turn has nothing to
// evaluate, so we drop the line before parsing rather than editing the file.
var emptyRackPass = regexp.MustCompile(`^>[^:]+:\s+-\s+\+0\s+-?\d+\s*$`)

// parseGCGLenient reads a GCG, strips empty-rack pass lines, and parses the rest.
func parseGCGLenient(cfg *config.Config, path string) (*pb.GameHistory, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(string(data), "\n")
	kept := lines[:0]
	for _, ln := range lines {
		if emptyRackPass.MatchString(strings.TrimRight(ln, "\r")) {
			continue
		}
		kept = append(kept, ln)
	}
	return gcgio.ParseGCGFromReader(cfg, strings.NewReader(strings.Join(kept, "\n")))
}

// bestEquity generates every legal play (plus pass, plus exchanges under FISE)
// and returns the highest static equity and the play achieving it.
func bestEquity(gd *kwg.KWG, calc *equity.CombinedStaticCalculator, g *game.Game,
	rack *tilemapping.Rack, ld *tilemapping.LetterDistribution, spanish bool) (float64, *move.Move) {

	gen := movegen.NewGordonGenerator(gd, g.Board(), ld)
	gen.SetGenPass(true)
	addExch := spanish && g.Bag().TilesRemaining() > 0
	if addExch {
		gen.SetMaxCanExchange(game.MaxCanExchange(g.Bag().TilesRemaining(), g.ExchangeLimit()))
	}
	plays := gen.GenAll(rack, addExch)

	opp := g.RackFor(1 - g.PlayerOnTurn())
	best := -1e18
	var bestMove *move.Move
	for _, p := range plays {
		e := calc.Equity(p, g.Board(), g.Bag(), opp)
		if e > best {
			best = e
			bestMove = p
		}
	}
	return best, bestMove
}

// drawPool reconstructs the multiset of tiles the player drew from when refilling
// after their previous play: the bag as it stood right after that play (before
// the refill), i.e. the current post-refill bag plus the tiles actually drawn.
// For a player's opening draw (no previous play) it approximates the pool as the
// current bag plus the drawn tiles.
func drawPool(history *pb.GameHistory, rules *game.GameRules, g *game.Game,
	prevPlayIdx int, drawn []tilemapping.MachineLetter) []tilemapping.MachineLetter {

	var bagTiles []tilemapping.MachineLetter
	if prevPlayIdx < 0 {
		bagTiles = g.Bag().Peek()
	} else {
		gPrev, err := game.NewFromHistory(history, rules, prevPlayIdx+1)
		if err != nil {
			return nil
		}
		bagTiles = gPrev.Bag().Peek()
	}
	pool := append([]tilemapping.MachineLetter(nil), bagTiles...)
	pool = append(pool, drawn...)
	return pool
}

// sampleWithout returns k tiles drawn without replacement from pool.
func sampleWithout(pool []tilemapping.MachineLetter, k int, rng *rand.Rand) []tilemapping.MachineLetter {
	cp := append([]tilemapping.MachineLetter(nil), pool...)
	rng.Shuffle(len(cp), func(i, j int) { cp[i], cp[j] = cp[j], cp[i] })
	return cp[:k]
}

// multisetDiff returns a - b as a multiset of machine letters.
func multisetDiff(a tilemapping.MachineWord, b []tilemapping.MachineLetter) []tilemapping.MachineLetter {
	counts := map[tilemapping.MachineLetter]int{}
	for _, t := range a {
		counts[t]++
	}
	for _, t := range b {
		counts[t]--
	}
	var out []tilemapping.MachineLetter
	for t, c := range counts {
		for ; c > 0; c-- {
			out = append(out, t)
		}
	}
	return out
}

// sortTiles orders tiles so counterfactual sampling is deterministic given the
// RNG seed (multisetDiff and Bag().Peek() do not guarantee a stable order).
func sortTiles(t []tilemapping.MachineLetter) {
	sort.Slice(t, func(i, j int) bool { return t[i] < t[j] })
}

func analyzable(evt *pb.GameEvent) bool {
	return evt.Type == pb.GameEvent_TILE_PLACEMENT_MOVE ||
		evt.Type == pb.GameEvent_EXCHANGE ||
		evt.Type == pb.GameEvent_PASS
}

func actionLabel(m *move.Move) string {
	switch m.Action() {
	case move.MoveTypePlay:
		return "play"
	case move.MoveTypeExchange:
		return "exch"
	case move.MoveTypePass:
		return "pass"
	default:
		return "?"
	}
}

func report(recs []turnRec, players []*pb.PlayerInfo, finalScore [2]int, luck bool) {
	name := func(i int) string {
		if i < len(players) {
			return players[i].Nickname
		}
		return fmt.Sprintf("P%d", i)
	}

	fmt.Println()
	hdr := fmt.Sprintf("%-4s %-12s %-9s %-5s %8s %8s %8s %8s", "#", "Player", "Rack", "Move", "Potent.", "Played", "Loss", "LeaveV")
	if luck {
		hdr += fmt.Sprintf(" %8s", "DrawLuck")
	}
	fmt.Println(hdr)
	fmt.Println(rule(len(hdr)))
	for _, r := range recs {
		line := fmt.Sprintf("%-4d %-12s %-9s %-5s %8.1f %8.1f %8.1f %8.1f",
			r.idx, trunc(name(r.player), 12), trunc(r.rack, 9), r.action,
			r.potential, r.played, r.loss, r.leaveVal)
		if luck {
			if r.hasLuck {
				line += fmt.Sprintf(" %+8.1f", r.drawLuck)
			} else {
				line += fmt.Sprintf(" %8s", "-")
			}
		}
		fmt.Println(line)
	}

	// Aggregate per player.
	type agg struct {
		n                       int
		sumPot, sumLoss, sumLck float64
		lckN                    int
	}
	var a [2]agg
	for _, r := range recs {
		a[r.player].n++
		a[r.player].sumPot += r.potential
		a[r.player].sumLoss += r.loss
		if r.hasLuck {
			a[r.player].sumLck += r.drawLuck
			a[r.player].lckN++
		}
	}

	fmt.Println()
	fmt.Println("Summary (all figures in equity points)")
	fmt.Println(rule(78))
	sh := fmt.Sprintf("%-12s %6s %10s %10s %10s", "Player", "Turns", "MeanPot.", "MeanLoss", "Score")
	if luck {
		sh += fmt.Sprintf(" %10s", "TotDrawLk")
	}
	fmt.Println(sh)
	for i := 0; i < 2; i++ {
		if a[i].n == 0 {
			continue
		}
		s := fmt.Sprintf("%-12s %6d %10.1f %10.1f %10d",
			trunc(name(i), 12), a[i].n, a[i].sumPot/float64(a[i].n), a[i].sumLoss/float64(a[i].n), finalScore[i])
		if luck {
			s += fmt.Sprintf(" %+10.1f", a[i].sumLck)
		}
		fmt.Println(s)
	}

	fmt.Println()
	fmt.Println("How to read it:")
	fmt.Println("  MeanPot.  higher = better racks/positions faced (opportunity, i.e. luck).")
	fmt.Println("  MeanLoss  lower  = captured more of that potential (efficiency, i.e. skill).")
	if luck {
		fmt.Println("  TotDrawLk positive = drew better than random refills would have (tile luck).")
	}

	if a[0].n > 0 && a[1].n > 0 {
		fmt.Println()
		fmt.Println(verdict(name, a[0].sumPot/float64(a[0].n), a[1].sumPot/float64(a[1].n),
			a[0].sumLoss/float64(a[0].n), a[1].sumLoss/float64(a[1].n), finalScore))
	}
}

// verdict flags the "did more with less" case: a player who both faced worse
// racks (lower mean potential) and played more efficiently (lower mean loss).
func verdict(name func(int) string, pot0, pot1, loss0, loss1 float64, score [2]int) string {
	better := 0
	if loss1 < loss0 {
		better = 1
	}
	other := 1 - better
	tag := fmt.Sprintf("%s was the more efficient player (mean loss %.1f vs %.1f).",
		name(better), leastLoss(loss0, loss1), maxLoss(loss0, loss1))
	potBetter := pot0
	potOther := pot1
	if better == 1 {
		potBetter, potOther = pot1, pot0
	}
	if potBetter < potOther {
		tag += fmt.Sprintf(" And did it with WORSE racks (mean potential %.1f vs %.1f) — that is doing more with less.",
			potBetter, potOther)
	} else {
		tag += fmt.Sprintf(" (They also had the better racks: mean potential %.1f vs %.1f, so efficiency and luck agreed.)",
			potBetter, potOther)
	}
	_ = other
	return tag
}

func leastLoss(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
func maxLoss(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func rule(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = '-'
	}
	return string(b)
}
