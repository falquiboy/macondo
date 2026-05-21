package tinymove

import (
	"fmt"

	"github.com/domino14/word-golib/tilemapping"
)

// A SmallMove consists of a TinyMove which encodes all the positional
// and tile info, plus a few extra fields needed for endgames and related
// modules. We're trying to strike a balance between decreasing allocations
// and still staying speedy.
type SmallMove struct {
	tm             TinyMove
	score          int16
	estimatedValue int16
	action         uint8
	// tilesDescriptor:
	// CCCC CPPP
	// 7    3
	// PPP = a 3-bit number (number of tiles in play that came from rack)
	// CCCCC = a 5-bit number (total number of tiles in play, including play-through)
	tilesDescriptor uint8
}

// DefaultSmallMove is a blank move (a pass)
var DefaultSmallMove = SmallMove{}

const tilesPlayedBitMask = 0b00000111

const (
	SmallMoveTypePass uint8 = iota
	SmallMoveTypePlay
	SmallMoveTypeExchange
)

func PassMove() SmallMove {
	// everything is 0.
	return SmallMove{}
}

func TilePlayMove(tm TinyMove, score int16, tilesPlayed, playLength uint8) SmallMove {
	tilesDescriptor := tilesPlayed + (playLength << 3)

	return SmallMove{tm: tm, score: score, action: SmallMoveTypePlay, tilesDescriptor: tilesDescriptor}
}

func ExchangeMove(tiles []tilemapping.MachineLetter) SmallMove {
	var moveCode uint64
	var blanksMask int
	bts := 20
	for idx, tile := range tiles {
		val := tile
		if tile == 0 || tile.IsBlanked() {
			blanksMask |= (1 << idx)
			val = tile.Unblank()
		}
		moveCode |= (uint64(val) << bts)
		bts += 6
	}
	// Pack the per-slot blank flags into bits 12..18 of the TinyMove,
	// matching the schema ExchangeTiles decodes via BlanksBitMask. Without
	// this, designated blanks fed to ExchangeMove would round-trip as their
	// underlying letter instead of as a blank (MachineLetter(0)). Undesignated
	// blanks already round-trip correctly because their slot value is 0.
	moveCode |= uint64(blanksMask) << 12
	tilesExchanged := uint8(len(tiles))
	tilesDescriptor := tilesExchanged + (tilesExchanged << 3)
	return SmallMove{
		tm:              TinyMove(moveCode),
		action:          SmallMoveTypeExchange,
		tilesDescriptor: tilesDescriptor,
	}
}

// EstimatedValue is an internal value that is used in calculating endgames and related metrics.
func (m *SmallMove) EstimatedValue() int16 {
	return m.estimatedValue
}

func (m *SmallMove) ShortDescription() string {
	if m.IsExchange() {
		return fmt.Sprintf("<tinyexchange: %d ntiles: %d>", m.tm, m.TilesPlayed())
	}
	// depends on the board.
	return fmt.Sprintf("<tinyplay: %d score: %d nracktiles: %d nplaytiles: %d>",
		m.tm,
		m.score, m.tilesDescriptor&tilesPlayedBitMask, m.tilesDescriptor>>3)
}

// SetEstimatedValue sets the estimated value of this move. It is calculated
// outside of this package.
func (m *SmallMove) SetEstimatedValue(v int16) {
	m.estimatedValue = v
}

// AddEstimatedValue adds an estimate to the existing estimated value of this
// estimate. Estimate.
func (m *SmallMove) AddEstimatedValue(v int16) {
	m.estimatedValue += v
}

func (m *SmallMove) TilesPlayed() int {
	return int(m.tilesDescriptor) & tilesPlayedBitMask
}

func (m *SmallMove) PlayLength() int {
	return int(m.tilesDescriptor) >> 3
}

func (m *SmallMove) Score() int {
	return int(m.score)
}

func (m *SmallMove) TinyMove() TinyMove {
	return m.tm
}

func (m *SmallMove) IsPass() bool {
	return m.action == SmallMoveTypePass && m.tm == 0
}

func (m *SmallMove) IsExchange() bool {
	return m.action == SmallMoveTypeExchange
}

func (m *SmallMove) IsTilePlay() bool {
	return m.action == SmallMoveTypePlay
}

func (m *SmallMove) ExchangeTiles(dst []tilemapping.MachineLetter) []tilemapping.MachineLetter {
	dst = dst[:0]
	if !m.IsExchange() {
		return dst
	}
	blanksMask := int(m.tm & BlanksBitMask)
	for idx := 0; idx < m.TilesPlayed(); idx++ {
		shifted := uint64(m.tm) & TBitMasks[idx]
		tile := tilemapping.MachineLetter(shifted >> tilemapping.MachineLetter(20+6*idx))
		if blanksMask&(1<<(idx+12)) > 0 {
			tile = 0
		}
		dst = append(dst, tile)
	}
	return dst
}

func (m *SmallMove) CoordsAndVertical() (int, int, bool) {
	// assume it's a tile play move
	t := m.tm
	row := int(t&RowBitMask) >> 6
	col := int(t&ColBitMask) >> 1
	vert := false
	if t&1 > 0 {
		vert = true
	}
	return row, col, vert
}
