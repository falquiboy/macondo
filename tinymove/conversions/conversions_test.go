package conversions

import (
	"testing"

	"github.com/domino14/word-golib/tilemapping"
	"github.com/matryer/is"

	"github.com/domino14/macondo/board"
	"github.com/domino14/macondo/cgp"
	"github.com/domino14/macondo/config"
	"github.com/domino14/macondo/move"
	"github.com/domino14/macondo/testhelpers"
	"github.com/domino14/macondo/tinymove"
)

var DefaultConfig = config.DefaultConfig()

func TestTinyMove(t *testing.T) {
	is := is.New(t)
	deepEndgame := "IBADAT1B7/2CAFE1OD1TRANQ/2TUT2RENIED2/3REV2YOMIM2/4RAFT1NISI2/5COR2N1x2/6LA1AGEE2/6LIAISED2/5POKY2W3/4JOWS7/V2LUZ9/ORPIN10/L1OE11/TUX12/I14 EEEEGH?/AGHNOSU 308/265 0 lex CSW19;"
	g, err := cgp.ParseCGP(DefaultConfig, deepEndgame)
	is.NoErr(err)

	m := move.NewScoringMove(0, []tilemapping.MachineLetter{0, 21 | 0x80},
		nil, false, 1, g.Alphabet(), 5, 12)

	tm := MoveToTinyMove(m)
	m2 := &move.Move{}
	TinyMoveToMove(tm, g.Board(), m2)
	is.True(move.MinimallyEqual(m, m2))
}

func TestComplexTinyMove(t *testing.T) {
	is := is.New(t)
	b := board.MakeBoard(board.CrosswordGameBoard)
	b.SetToGame(testhelpers.EnglishAlphabet(), board.VsMatt2)

	m := move.NewScoringMove(0,
		[]tilemapping.MachineLetter{5, 0, 4 | 0x80, 0, 5},
		nil, true, 3, testhelpers.EnglishAlphabet(), 9, 9)

	tm := MoveToTinyMove(m)
	m2 := &move.Move{}
	TinyMoveToMove(tm, b, m2)
	is.True(move.MinimallyEqual(m, m2))
}

func TestSmallExchangeMoveToMove(t *testing.T) {
	is := is.New(t)
	alph := testhelpers.EnglishAlphabet()
	rack := tilemapping.RackFromString("ABCDEF?", alph)

	sm := tinymove.ExchangeMove([]tilemapping.MachineLetter{1, 0, 3})
	m := &move.Move{}
	SmallMoveToMove(sm, m, alph, board.MakeBoard(board.CrosswordGameBoard), rack)

	is.Equal(m.Action(), move.MoveTypeExchange)
	is.Equal(m.TilesStringExchange(), "A?C")
	is.Equal(m.LeaveString(), "BDEF")
	is.Equal(m.TilesPlayed(), 3)
}

// TestExchangeMoveDesignatedBlankRoundTrip locks down a latent bug where
// ExchangeMove computed a per-slot blank mask but never packed it into the
// TinyMove. Undesignated blanks (MachineLetter(0)) round-tripped correctly
// by coincidence (the slot value was already 0), but a designated blank
// such as `A | 0x80` would lose its blank flag and decode as the
// underlying letter. With the mask now packed into bits 12..18 the round
// trip is exact for both forms.
func TestExchangeMoveDesignatedBlankRoundTrip(t *testing.T) {
	is := is.New(t)
	alph := testhelpers.EnglishAlphabet()

	// Slot 1 is a designated blank standing in for A; slot 0 is a regular
	// tile, slot 2 is an undesignated blank.
	designated := tilemapping.MachineLetter(1) | 0x80
	tiles := []tilemapping.MachineLetter{2, designated, 0}
	sm := tinymove.ExchangeMove(tiles)
	is.Equal(sm.IsExchange(), true)
	is.Equal(sm.TilesPlayed(), 3)

	var buf [7]tilemapping.MachineLetter
	got := sm.ExchangeTiles(buf[:0])
	is.Equal(len(got), 3)
	is.Equal(got[0], tilemapping.MachineLetter(2)) // regular B
	is.Equal(got[1], tilemapping.MachineLetter(0)) // designated blank → blank
	is.Equal(got[2], tilemapping.MachineLetter(0)) // undesignated blank
	_ = alph
}
