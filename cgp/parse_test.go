package cgp

import (
	"testing"

	"github.com/matryer/is"

	"github.com/domino14/macondo/config"
	"github.com/domino14/macondo/testhelpers"
	"github.com/domino14/word-golib/tilemapping"
)

var DefaultConfig = config.DefaultConfig()

func TestRowToLetters(t *testing.T) {
	is := is.New(t)
	testcases := []struct {
		row    string
		parsed []tilemapping.MachineLetter
	}{
		{"15", []tilemapping.MachineLetter{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}},
		{"10AB3", []tilemapping.MachineLetter{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 2, 0, 0, 0}},
		{"A3B10", []tilemapping.MachineLetter{1, 0, 0, 0, 2, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}},
		{"1A1B2C3D4", []tilemapping.MachineLetter{0, 1, 0, 2, 0, 0, 3, 0, 0, 0, 4, 0, 0, 0, 0}},
	}
	for _, tc := range testcases {
		parsed, err := rowToLetters(tc.row, testhelpers.EnglishAlphabet())
		is.NoErr(err)
		is.Equal(parsed, tc.parsed)
	}
}

func TestRowToLettersMultichar(t *testing.T) {
	is := is.New(t)
	catalan, err := tilemapping.NamedLetterDistribution(DefaultConfig.WGLConfig(), "catalan")
	is.NoErr(err)
	testcases := []struct {
		row    string
		parsed []tilemapping.MachineLetter
	}{
		{"15", []tilemapping.MachineLetter{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}},
		{"10AB3", []tilemapping.MachineLetter{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 2, 0, 0, 0}},
		{"A3b10", []tilemapping.MachineLetter{1, 0, 0, 0, 2 | 0x80, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}},
		{"1A1B2C3D4", []tilemapping.MachineLetter{0, 1, 0, 2, 0, 0, 3, 0, 0, 0, 5, 0, 0, 0, 0}},
		{"[qu]3[L·L]5MA3", []tilemapping.MachineLetter{19 | 0x80, 0, 0, 0, 13, 0, 0, 0, 0, 0, 14, 1, 0, 0, 0}},
		{"[qu]3[L·L]5MA2Ç", []tilemapping.MachineLetter{19 | 0x80, 0, 0, 0, 13, 0, 0, 0, 0, 0, 14, 1, 0, 0, 4}},
		{"[QU]3[l·l]9[NY]", []tilemapping.MachineLetter{19, 0, 0, 0, 13 | 0x80, 0, 0, 0, 0, 0, 0, 0, 0, 0, 16}},
		{"QU3l·l9NY", []tilemapping.MachineLetter{19, 0, 0, 0, 13 | 0x80, 0, 0, 0, 0, 0, 0, 0, 0, 0, 16}},
	}
	for _, tc := range testcases {
		parsed, err := rowToLetters(tc.row, catalan.TileMapping())
		is.NoErr(err)
		is.Equal(parsed, tc.parsed)
	}
}

func TestParseSpanishFILE2017CGP(t *testing.T) {
	is := is.New(t)
	cfg := config.DefaultConfig()
	cfg.AdjustRelativePaths("..")

	cgp := "BRO[CH]A2C3VEJO/3U1D1RO1FALO1/2ET1I1EX1AHE2/COSO1G1Y3E3/2T2APO[RR]eAIS2/1ZA3L4S3/2N2QUAD6/2C3ME7/1PANDEAS7/1UN1EH9/3DA10/URSINAs8/R5UTERINO2/G14/I14 ACDILÑS/BEEELMO 382/405 0 lex FILE2017"
	g, err := ParseCGP(cfg, cgp)
	is.NoErr(err)
	is.Equal(g.LexiconName(), "FILE2017")
	is.Equal(g.Rules().LetterDistributionName(), "spanish")
	is.Equal(g.RackFor(0).String(), "ACDILÑS")
	is.Equal(g.RackFor(1).String(), "BEEELMO")
	is.Equal(g.PlayerOnTurn(), 0)
}
