package preendgame

import (
	"context"
	"testing"
	"time"

	"github.com/domino14/word-golib/kwg"
	"github.com/domino14/word-golib/tilemapping"
	"github.com/matryer/is"

	"github.com/domino14/macondo/cgp"
	"github.com/domino14/macondo/move"
)

// TestPEGSpanishExchangesPostOppFillCap is a regression test for a state-stack
// leak that occurred when configurePEGMovegen capped exchange size based on
// the bag *before* the opponent's rack was filled. With unseen=9 the
// pre-fill bag held 9 tiles, so the generator emitted exchanges of size
// 1–7. Per-permutation evaluation later set the opp rack (bag → 2),
// causing PlayMove to reject any exchange of size > 2 and leaking one
// state-stack entry per rejection (backupState ran before the validity
// check, UnplayLastMove was skipped on error). Eventually the stack
// overflowed with "index out of range" on backupState.
//
// The fix caps the top-level MaxCanExchange to the post-opp-fill bag size
// so the generator never emits an exchange PlayMove will reject.
func TestPEGSpanishExchangesPostOppFillCap(t *testing.T) {
	is := is.New(t)

	cgpStr := "BRO[CH]A2C3VEJO/3U1D1RO1FALO1/2ET1I1EX1AHE2/COSO1G1Y3E3/" +
		"2T2APO[RR]eAIS2/1ZA3L4S3/2N2QUAD6/2C3ME7/1PANDEAS7/" +
		"1UN1EH9/3DA10/URSINAs8/R5UTERINO2/G14/I14 " +
		"ACDILÑS/ 0/0 0 lex FILE2017;"

	g, err := cgp.ParseCGP(DefaultConfig, cgpStr)
	if err != nil {
		t.Skipf("ParseCGP failed (likely missing FILE2017 lexicon data): %v", err)
	}
	g.RecalculateBoard()

	gd, err := kwg.GetKWG(DefaultConfig.WGLConfig(), "FILE2017")
	if err != nil {
		t.Skipf("FILE2017 KWG not available: %v", err)
	}

	peg := new(Solver)
	err = peg.Init(g.Game, gd)
	is.NoErr(err)
	peg.SetThreads(2)
	peg.SetEndgamePlies(1)
	peg.SetIterativeDeepening(false)
	peg.SetMaxTilesLeft(-1)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	plays, err := peg.Solve(ctx)
	is.NoErr(err) // must not panic / return error

	// At least one play must be present and the candidate pool must include
	// both tile plays and exchanges (FILE2017 lets us exchange under FISE
	// rules even with bag ≤ 6, capped here to bag-after-opp-fill = 2).
	is.True(len(plays) > 0)

	sawTile, sawExch := false, false
	maxExch := 0
	for _, p := range plays {
		switch p.Play.Action() {
		case move.MoveTypePlay:
			sawTile = true
		case move.MoveTypeExchange:
			sawExch = true
			if n := p.Play.TilesPlayed(); n > maxExch {
				maxExch = n
			}
		}
	}
	is.True(sawTile)
	is.True(sawExch)
	// With 9 unseen and 7 going to opp, the bag holds 2 tiles. The largest
	// exchange we should ever emit is 2 — anything bigger would crash
	// per-perm evaluation.
	is.True(maxExch <= 2)
}

// TestPEGSpanishExchangeOutcomeConsistency is a regression test for a
// double-counting bug in exchange evaluation. With 1 tile in the bag, an
// outer PEG over Spanish has 8 distinct opp-tile scenarios (one per unseen
// tile). For any single-move solve (-only-solve), TotalOutcomes() must equal
// 8 multiplied by a clean post-exchange branching factor (1 or the number of
// post-shuffle bag permutations). The buggy state observed at endgame ply 1
// reports TotalOutcomes=15 (8 wins under the extended branchOption.mls key +
// 7 losses under the original inbagOption.mls key), making %Win = 8/15 =
// 53.33% and inflating PointRate above genuine tile plays.
//
// Position: post-Spanish-opening; rack ABCEGJL trailing 374-409 with 1
// unseen pool of {A,C,D,E,H,I,N,T}. Only opp=A could give the side a
// theoretical win; the deeper search (ply ≥ 2) correctly returns 0 wins
// for (exch C).
//
// The test asserts the basic invariant: TotalOutcomes() % numOppScenarios
// == 0, where numOppScenarios = 8 here. Currently fails at ply 1, passes
// at ply ≥ 2.
func TestPEGSpanishExchangeOutcomeConsistency(t *testing.T) {
	is := is.New(t)

	cgpStr := "3B11/3U8SE1/3G8OH1/3L8N2/3EA7R1I/4C7E1Z/" +
		"4O1A1UtOPICO/1M2L1PUYO2R1T/1A2[CH]1R7E/" +
		"ASOMARES1Q3X1/1E2S1S2U2TI1/TA1DESA[RR]IENDA2/" +
		"AD4R5L2/ÑO3FA5U2/Es2[LL]ENO2VIDON " +
		"ABCEGJL/ 374/409 0 lex FILE2017;"

	g, err := cgp.ParseCGP(DefaultConfig, cgpStr)
	if err != nil {
		t.Skipf("ParseCGP failed (likely missing FILE2017 lexicon data): %v", err)
	}
	g.RecalculateBoard()

	gd, err := kwg.GetKWG(DefaultConfig.WGLConfig(), "FILE2017")
	if err != nil {
		t.Skipf("FILE2017 KWG not available: %v", err)
	}

	alph := g.Alphabet()
	exchanged := tilemapping.RackFromString("C", alph).TilesOn()
	leave := tilemapping.RackFromString("ABEGJL", alph).TilesOn()
	exchMove := move.NewExchangeMove(exchanged, leave, alph)

	// Number of distinct opp-tile scenarios = unseen tiles when bag has 1.
	// Opponent's full rack of 7 is hidden, plus 1 in bag = 8 unseen tiles,
	// all distinct here.
	const numOppScenarios = 8

	runAtPly := func(plies int) *PreEndgamePlay {
		peg := new(Solver)
		err := peg.Init(g.Game, gd)
		is.NoErr(err)
		peg.SetThreads(2)
		peg.SetEndgamePlies(plies)
		peg.SetIterativeDeepening(false)
		peg.SetSolveOnly([]*move.Move{exchMove})

		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		plays, err := peg.Solve(ctx)
		is.NoErr(err)
		is.True(len(plays) >= 1)
		return plays[0]
	}

	ply1 := runAtPly(1)
	ply4 := runAtPly(4)

	t.Logf("ply1: Points=%.2f FoundLosses=%.2f TotalOutcomes=%d PointRate=%.4f",
		ply1.Points, ply1.FoundLosses, ply1.TotalOutcomes(), ply1.PointRate())
	t.Logf("ply4: Points=%.2f FoundLosses=%.2f TotalOutcomes=%d PointRate=%.4f",
		ply4.Points, ply4.FoundLosses, ply4.TotalOutcomes(), ply4.PointRate())

	// Invariant #1: TotalOutcomes must be a clean multiple of the number of
	// outer opp-tile scenarios. Asymmetric enumeration (wins under extended
	// key, losses under original key) produces a non-multiple — the bug.
	is.Equal(ply1.TotalOutcomes()%numOppScenarios, 0)
	is.Equal(ply4.TotalOutcomes()%numOppScenarios, 0)

	// Invariant #2: Points + FoundLosses == TotalOutcomes. Every outcome
	// entry contributes its count to exactly one of {Points, FoundLosses}
	// (with PEGDraw splitting halves). For a position with no draws this
	// is a strict equality.
	is.Equal(int(ply1.Points+ply1.FoundLosses), ply1.TotalOutcomes())
	is.Equal(int(ply4.Points+ply4.FoundLosses), ply4.TotalOutcomes())

	// Invariant #3: PointRate is bounded by [0, 1]. With 8 wins reported
	// against 15 outcomes (bug state), the rate is 0.5333 — technically
	// in range but only because the inflation happens on both sides.
	// What matters is that the rate at ply 1 should not exceed the rate
	// of a genuinely winning play in the same position. Tile plays here
	// peak at 1/8 = 0.125; the exchange's true rate is also ≤ 0.125
	// (it loses 7/8 scenarios outright and at best ties the 8th).
	is.True(ply1.PointRate() <= 0.125+1e-4)
}

// TestPEGSpanish3InBagExchangeNoPanic is a regression test for a state-stack
// overflow that crashes the solver on a 3-tile-in-bag Spanish position where
// exchanges are legal. The panic surfaces as:
//
//	panic: runtime error: index out of range [57] with length 57
//	  game/backup.go:46  (st := g.stateStack[g.stackPtr])
//	  ...
//	  preendgame/peg_generic.go recursiveSolveExchange → continueAfterPEGMove
//	  → iterateOurReplies → nestedOurTurnSolve
//
// Root cause: the state-stack length set in Solve()
//
//	g.SetStateStackLength(game.DefaultMaxScorelessTurns*(s.numinbag+1) +
//	                      s.curEndgamePlies + 10)
//
// bounds scoreless-turn runs at DefaultMaxScorelessTurns (6) per bag level.
// With 3 in the bag and exchanges legal for both sides, the nested PEG
// recursion (recursiveSolveExchange replays the exchange and descends into
// opponent replies, our replies, and further nested solves) pushes more
// backup frames than that bound anticipates, so g.stackPtr runs past the
// end of g.stateStack. This is the same *class* of bug fixed in 52cc48f
// for the top-level per-perm path, but reached via the nested-solve path
// that fix did not cover.
//
// The earlier 52cc48f fix sized the stack for the top-level exchange path;
// this position exercises the deeper nested recursion. The test asserts the
// solver completes without panicking. It is skipped (rather than left to
// crash the whole package binary, since the panic originates in a worker
// goroutine and re-panics through handleJobGeneric's recover) until the
// stack sizing covers the nested exchange depth.
func TestPEGSpanish3InBagExchangeNoPanic(t *testing.T) {
	t.Skip("KNOWN CRASH: state-stack overflow in nested exchange recursion " +
		"(game/backup.go:46). Un-skip once Solve() sizes the stack for the " +
		"nested-solve exchange depth, not just the top-level per-perm path.")

	is := is.New(t)

	cgpStr := "3HA[CH]EES6/3U11/2HILADOR6/3L3C7/2MOFO1UNCE4/" +
		"6OLEEN4/5S1T2R1T2/AEROLITO1BI[RR]EMe/P4U4A1R2/" +
		"AJ3X3ADUCEN/RA1O6O1E1I/CIÑAS7T1E/ASA9O1G/" +
		"D11S1A/o13N DENPUYZ/ 409/407 0 lex FILE2017;"

	g, err := cgp.ParseCGP(DefaultConfig, cgpStr)
	if err != nil {
		t.Skipf("ParseCGP failed (likely missing FILE2017 lexicon data): %v", err)
	}
	g.RecalculateBoard()

	gd, err := kwg.GetKWG(DefaultConfig.WGLConfig(), "FILE2017")
	if err != nil {
		t.Skipf("FILE2017 KWG not available: %v", err)
	}

	peg := new(Solver)
	err = peg.Init(g.Game, gd)
	is.NoErr(err)
	peg.SetThreads(2)
	peg.SetEndgamePlies(2)
	peg.SetIterativeDeepening(true)

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	// Currently panics inside a worker goroutine before returning.
	_, err = peg.Solve(ctx)
	is.NoErr(err)
}
