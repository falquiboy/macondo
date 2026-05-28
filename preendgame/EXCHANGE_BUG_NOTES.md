# PEG Exchange — Double-Counting Bug

**Status:** Fixed with approach A. Regression coverage lives in
`TestPEGSpanishExchangeOutcomeConsistency` in `peg_spanish_regression_test.go`.

**Affects:** Spanish PEG (and any future configuration where
`MaxCanExchange(bag, ExchangeLimit) > 0`). English/CSW unaffected because
their `ExchangeLimit = 7` gates exchanges out for any bag a PEG would ever
encounter.

**Manifests at:** `peg -endgameplies 1`. Higher plies hide it because
deeper endgame search finds losses early enough to trigger the existing
`HasLoss` cutoff in `recursiveSolve` (line 1006) before the buggy
enumeration runs to completion. Iterative deepening therefore *appears*
to converge to the right answer, but only by accident.

---

## Symptom

For a 1-tile-in-bag Spanish PEG with rack `ABCEGJL`, score 374–409, run
`peg -only-solve "exch C" -endgameplies 1 -disable-id true`. Result:

```
(exch C)            8.0     53.33   👍: CA CCA DCA ECA HCA ICA NCA TCA
                                    👎: CC CD CE CH CI CN CT
```

- `Points = 8.0`, `FoundLosses = 7.0`, `TotalOutcomes = 15`
- True count of distinct outer scenarios = 8 (one per unseen tile the opponent
  might hold). 15 ≠ a clean multiple of 8.
- The same scenario (opponent holds tile X ≠ A) is recorded *twice*:
  once as a 3-letter label under the extended `branchOption.mls` key
  (wins side) and once as a 2-letter label under the original
  `inbagOption.mls` key (loss side).

At `-endgameplies 4` the same call returns
`Points=0, FoundLosses=8, TotalOutcomes=8` — correct shape, because the
deeper endgame leaf identifies the loss before the post-exchange
enumeration ever recurses.

---

## Root cause

`recursiveSolveExchange` ([`peg_generic.go:1187`](peg_generic.go))
expands post-exchange branches by computing
`branchOption.mls = pegBranchKey(inbagOption.mls, postTiles)` (line 1215)
and passing `branchOption` down through `continueAfterPEGMove`. The
recursive descent into `iterateOppReplies → recursiveSolve → endgame
leaf` records the verdict via `addWinPctStat`/`setUnfinalizedWinPctStat`
keyed under the *extended* `branchOption.mls`.

Separately, somewhere on the same descent — most likely on an early-loss
path inside `iterateOppReplies` (line 1281 `HasLoss(inbagOption.mls)`
check) or in a frame that captured the outer `inbagOption` *before* the
branch expansion — a loss is recorded under the *original*
`inbagOption.mls`. The two entries coexist in `outcomesArray`, each
contributing to `noutcomes`.

Net effect on the sort/tiebreak:

- `PointRate = Points / TotalOutcomes` = 8 / 15 = **53.33%**
- Tile plays in the same position score 1/8 = **12.5%**
- The exchange wins the `betterPreEndgamePlay` comparison even though it
  loses every real scenario except the one where opp holds the A.

The `52cc48f` refinements note (kept in
`macondo-spanish-peg-refinements.md`) explicitly chose `PointRate` over
absolute `Points` to handle the legitimate case where exchange and
tile-play outcome spaces differ in size. That choice is correct in
principle but does **not** rescue the case where the same scenario is
double-recorded, because the inflation lands on the numerator without
matching inflation on the denominator's *independent-scenario* portion.

---

## Fix direction

Approach A was applied. The other candidates are kept here for context.

### A. Collapse post-exchange branches at the exchange boundary (preferred)

Make `recursiveSolveExchange` evaluate each `postPerm` into a scratch
`PreEndgamePlay`, aggregate the per-branch verdicts into a single
`PEGWin / PEGDraw / PEGLoss` for the outer `inbagOption.mls` (pessimistic
across draw probability, since the draw is random — equivalent to:
WIN iff every branch wins; LOSS iff every branch loses; otherwise DRAW),
and record that one verdict on the real `pegPlay` keyed under
`inbagOption.mls`. Drop the extended-key recording entirely.

Pros: removes the asymmetry by construction. Keeps `noutcomes` aligned
with the number of opp scenarios across exchanges and tile plays.
Cons: refactor of the recursive descent; need to verify the scratch
aggregation matches what `nestedOurTurnSolve` does for non-exchange
non-bag-emptying moves (consistency between sibling paths).

### B. Force every record on the descent to key under `inbagOption.mls`

Plumb the outer `inbagOption` (not `branchOption`) through
`continueAfterPEGMove → iterateOppReplies → recursiveSolve →
addWinPctStat`. Sum the `branchOption.ct` weights into the outer entry
instead of creating a new entry per branch.

Pros: smaller diff, fewer moving parts.
Cons: loses per-branch resolution in the explain/trace output; not
obvious whether downstream consumers (HasLoss checks during sibling
branches) depend on the per-branch keys for correctness.

### C. Strip the extension at write time

Inside `addWinPctStat` / `setUnfinalizedWinPctStat`, if the play is an
exchange, truncate `tiles` back to `numinbag` before looking up
`outcomesArray`.

Pros: one-line change in two places.
Cons: hides the bug rather than fixing the structural asymmetry; will
break in any future use of branchKey for non-exchange purposes.

---

## Invariants the fix must preserve

The regression test asserts three invariants. All three must hold after
the fix lands (currently only the third is trivially true):

1. **Outcome-count cleanliness**: `TotalOutcomes() % numOppScenarios == 0`
   — every opp scenario contributes the same number of entries (1 if A
   is taken, k > 1 if B/C are taken).

2. **Wins+losses identity**: `Points + FoundLosses == TotalOutcomes`
   for positions without draws.

3. **PointRate bound**: the rate for any exchange must not exceed the
   genuine maximum achievable rate over the position (for the test
   position, 1/8 = 0.125 since only opp=A gives a winning shape, and
   even then only if the post-exchange continuation actually wins).

---

## When this matters operationally

Any time `peg` is run at `endgameplies 1` (the default in some contexts,
the value of `-endgameplies 1` explicitly, and the *first iteration of
iterative deepening at every other ply count*) on a Spanish position
where an exchange has at least one winning outer scenario, the exchange
will be reported as the recommended play even when it's the worst
candidate. Operators running `peg -endgameplies 1` or watching the
ply-1 line of iterative-deepening output will be misled.

Iterative deepening to ply ≥ 2 with the existing default arguments
produces the correct final answer, so users running the default
`peg` command without overriding plies will not see the wrong winner.
But the issue surfaces immediately if any of these are changed:
- `-endgameplies 1` (explicit shallow search)
- `-disable-id true` (iterative deepening off)
- Any future caller that consumes the per-iteration log line
  "iterative-deepening endgame-plies:1" winner field.

---

## Reproducer (one-liner)

```bash
MACONDO_DATA_PATH=$(pwd)/data go test ./preendgame/ \
  -run TestPEGSpanishExchangeOutcomeConsistency -v
```

Currently fails with `7 != 0` on the first assertion. Will pass when
the fix lands.

---
---

# PEG Exchange — State-Stack Overflow on 3-in-bag (separate bug)

**Status:** Fixed by making `game.backupState` grow the simulation stack on
demand. Regression coverage lives in `TestPEGSpanish3InBagExchangeNoPanic`.
This is a *different* bug from the double-counting one above — it is a hard
crash, not a wrong number.

**Affects:** Spanish PEG on positions with **≥ 3 tiles in the bag** where
exchanges are legal. The 1-in-bag cases above never reach the depth that
triggers it. English/CSW unaffected (no exchanges in PEG).

## Symptom

`peg -endgameplies 4 -threads 8 -maxtime 180` on:

```
3HA[CH]EES6/3U11/2HILADOR6/3L3C7/2MOFO1UNCE4/6OLEEN4/5S1T2R1T2/
AEROLITO1BI[RR]EMe/P4U4A1R2/AJ3X3ADUCEN/RA1O6O1E1I/CIÑAS7T1E/
ASA9O1G/D11S1A/o13N DENPUYZ/ 409/407 0 lex FILE2017;
```

(rack `DENPUYZ`, score 409-407, **bag = 3**, unseen pool = 10) panics:

```
panic: runtime error: index out of range [57] with length 57
  game/backup.go:46                      st := g.stateStack[g.stackPtr]
  preendgame/peg_generic.go:1606         nestedOurTurnSolve
  preendgame/peg_generic.go:1315         iterateOurReplies
  preendgame/peg_generic.go:1171         continueAfterPEGMove
  preendgame/peg_generic.go:1231         recursiveSolveExchange
  preendgame/peg_generic.go:1128         recursiveSolve
  preendgame/peg_generic.go:962          processJobPerPlay
  preendgame/peg_generic.go:784          handleJobGeneric
```

`g.stackPtr` runs off the end of `g.stateStack`.

## Root cause

`Solve()` sizes the per-endgame state stack (peg.go ≈ line 827):

```go
g.SetStateStackLength(game.DefaultMaxScorelessTurns*(s.numinbag+1) +
                      s.curEndgamePlies + 10)
```

`DefaultMaxScorelessTurns = 6`. The formula assumes each "bag level" admits
at most 6 scoreless turns before the six-scoreless-turn rule ends the game.
That bound is correct for the *top-level* PEG line, but the **nested**
solver (`recursiveSolveExchange → continueAfterPEGMove → iterateOurReplies
→ nestedOurTurnSolve`) descends through additional exchange/our-reply/
opp-reply frames, each calling `backupState` (a stack push). With 3 in the
bag and both sides able to exchange, the live push depth exceeds the
allocated length and `backupState` indexes past the end.

This is the same failure mode `52cc48f` fixed for the per-perm path
(backupState runs before validity checks, UnplayLastMove is skipped on
error → leaked pushes). That fix sized the stack for the top-level
exchange line; it did not account for the extra depth the nested solver
adds on multi-tile-in-bag positions.

## Fix direction

Option 2 was applied. The original options are kept here for context:

1. **Grow the bound to cover nested depth.** The nested recursion adds up
   to `nestedDepthLimit` extra plies, each of which can host its own
   scoreless run. A safe (if loose) bound multiplies the scoreless headroom
   by the nesting factor, e.g.
   `DefaultMaxScorelessTurns*(numinbag+1)*(nestedDepthLimit+1) +
    curEndgamePlies + 10`, or derive the true max push depth analytically
   from the recursion structure.

2. **Make `backupState` grow the stack on demand** instead of indexing a
   fixed slice — append a fresh `stateBackup` when `stackPtr` reaches
   `len(stateStack)`. This removes the whole class of "stack too small"
   crashes at the cost of a rare allocation. Cleaner long-term; touches
   `game/backup.go` shared by all callers, so needs broader review.

Option 2 is the more durable fix; option 1 is the smaller, PEG-local
change.

## Reproducer

```bash
# Un-skip TestPEGSpanish3InBagExchangeNoPanic first, then:
MACONDO_DATA_PATH=$(pwd)/data go test ./preendgame/ \
  -run TestPEGSpanish3InBagExchangeNoPanic -v
```

This test now passes once the backup stack can grow past the initial PEG
depth estimate.
