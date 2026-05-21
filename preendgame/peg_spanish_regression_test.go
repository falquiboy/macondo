package preendgame

import (
	"context"
	"testing"
	"time"

	"github.com/domino14/word-golib/kwg"
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
