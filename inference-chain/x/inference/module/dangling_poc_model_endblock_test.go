package inference

import (
	"testing"

	"github.com/productscience/inference/x/inference/types"
	"github.com/stretchr/testify/require"
)

// Consensus-halt audit (class 1 — error returned from EndBlocker) — GATE 3:
// the full integration proof. Drives the real am.EndBlock at a PoC-start
// height (IsStartOfPocStage == blockHeight == PocStartBlockHeight) with a
// dangling PoC model and asserts EndBlock RETURNS a non-nil error.
//
// A non-nil return from EndBlock is exactly what the Cosmos SDK treats as a
// fatal ABCI error: the block cannot be committed, so every node aborts at the
// same height => deterministic CHAIN HALT. This closes the chain:
//   keeper RED: a plain governance delete leaves PocParams.Models dangling (no guard)
//   module GATE 2: initializeUpcomingEpochModelGroups returns the error on that state
//   -> GATE 3 (here): that error actually propagates out of EndBlock.
//
// Reuses newFormationRecoveryFixture (effective epoch 1, PoC start 100). The
// only injected fault is one extra, unregistered model in PocParams.Models.
func TestEndBlock_DanglingPocModel_HaltsAtPoCStart(t *testing.T) {
	fixture := newFormationRecoveryFixture(t, noopCollateralKeeper{}, 0, 2)
	k := fixture.keeper

	// Baseline sanity: without the fault, EndBlock at the PoC-start height must
	// NOT halt. (Run this first on a clean copy of the fixture.)
	// Here we go straight to the fault case; the baseline is covered by the
	// package's existing formation tests.

	const ghostModel = "ghost-unregistered-model"

	params, err := k.GetParams(fixture.ctx)
	require.NoError(t, err)
	// The LIVE PoC config references exactly one model that is fully configured
	// (PREFILL+DECODE scheme blocks, so it passes the recipe/coefficient freeze
	// that precedes formation) but is NOT a governance model — the exact state a
	// governance delete or an unguarded MsgUpdateParams leaves behind. It trips
	// only the governance lookup inside initializeUpcomingEpochModelGroups.
	ghost := dynamicModel(ghostModel, nil, dec(1, 0), dec(1, 0), dec(1, 0), 10_000) // PREFILL block
	ghost.Schemes = append(ghost.Schemes, &types.PocSchemeParams{
		Scheme:      types.PocScheme_POC_SCHEME_DECODE,
		SeqLen:      128,
		DecodeSteps: 4,
		DynamicCoefficient: &types.DynamicCoefficientModelConfig{
			CoeffMin: dec(1, 0), CoeffMax: dec(1, 0), RelativeDifficulty: dec(1, 0), TargetShareBps: 10_000,
		},
	})
	params.PocParams.Models = []*types.PoCModelConfig{ghost}
	require.NoError(t, k.SetParams(fixture.ctx, params))

	if _, found := k.GetGovernanceModel(fixture.ctx, ghostModel); found {
		t.Fatalf("precondition: %q must be absent from governance models", ghostModel)
	}

	// Advance the effective epoch to the fixture's upcoming epoch (index 2) so the
	// formation at its PoC start targets a fresh epoch (index 3) whose group has
	// not been pre-seeded by the fixture (which only seeds epochs 1 and 2).
	current := fixture.upcomingEpoch
	require.NoError(t, k.SetEffectiveEpochIndex(fixture.ctx, current.Index))

	// The fixture ctx caches params (InjectParamsIntoContext); SetParams only
	// wrote the store, so refresh the cache or EndBlock reads the pre-mutation copy.
	ctx, err := k.InjectParamsIntoContext(fixture.ctx)
	require.NoError(t, err)

	// IsStartOfPocStage fires when blockHeight == the epoch context's
	// PocStartBlockHeight, which NewEpochContextFromEffectiveEpoch resolves to the
	// NEXT epoch's PoC start (ec.NextPoCStart()), not the current epoch's start.
	epochParams, err := k.GetParams(ctx)
	require.NoError(t, err)
	ec := types.NewEpochContext(current, *epochParams.EpochParams)
	formationHeight := ec.NextPoCStart()
	ctx = ctx.WithBlockHeight(formationHeight)

	err = fixture.module.EndBlock(ctx)
	require.Error(t, err,
		"EndBlock must return a fatal error on a dangling PoC model config -> the "+
			"SDK aborts the block => deterministic chain halt at the epoch boundary")
	require.Contains(t, err.Error(), "missing from governance models",
		"the halting error must originate from initializeUpcomingEpochModelGroups")
}
