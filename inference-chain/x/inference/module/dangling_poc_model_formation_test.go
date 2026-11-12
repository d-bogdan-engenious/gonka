package inference

import (
	"testing"

	"github.com/productscience/inference/x/inference/epochgroup"
	"github.com/productscience/inference/x/inference/types"
	"github.com/stretchr/testify/require"
)

// Consensus-halt audit (class 1 — error returned from EndBlocker) — GATE 2:
// executes the actual halt TRIGGER, not just the reachable predicate.
//
// Companion to keeper/dangling_poc_model_halt_test.go (which proves a plain
// governance delete leaves PocParams.Models referencing a now-missing
// governance model, with no guard). Here we drive the exact formation helper
// the next epoch boundary runs — initializeUpcomingEpochModelGroups — against
// that dangling state and confirm it returns the error that propagates:
//   initializeUpcomingEpochModelGroups (module.go:555)
//     -> onEndOfPoCValidationStage
//       -> EndBlock (module.go:497 "return err")
//         -> SDK aborts the block => deterministic CHAIN HALT.
//
// This is a characterization test (GREEN): it documents that the trigger fires
// today. GREEN-on-fix would require either rejecting the delete while
// referenced / scrubbing the reference, or formation skipping-with-log — at
// which point this test is updated to assert no error.
func TestInitializeUpcomingEpochModelGroups_DanglingModel_FiresHaltError(t *testing.T) {
	am, k, ctx := challengeApp(t)

	const danglingID = "dangling-poc-model"

	// Live PoC config references a model that is NOT a governance model —
	// the exact state a governance delete (or an unguarded MsgUpdateParams)
	// leaves behind.
	params, err := k.GetParams(ctx)
	require.NoError(t, err)
	params.PocParams.Models = []*types.PoCModelConfig{{ModelId: danglingID}}
	require.NoError(t, k.SetParams(ctx, params))

	_, found := k.GetGovernanceModel(ctx, danglingID)
	require.Falsef(t, found, "precondition: %q must be absent from governance models", danglingID)

	// Non-nil parent group so the nil-guard passes; the error fires before any
	// CreateSubGroup call, so a minimal group is sufficient.
	parentGroup := &epochgroup.EpochGroup{
		GroupData: &types.EpochGroupData{EpochIndex: 3},
	}

	err = am.initializeUpcomingEpochModelGroups(ctx, parentGroup)
	require.Error(t, err,
		"formation must surface the dangling PoC model; this error propagates to "+
			"EndBlock (module.go:497) and halts the chain at the epoch boundary")
	require.Contains(t, err.Error(), "missing from governance models",
		"the halt is triggered by the governance-model lookup in initializeUpcomingEpochModelGroups")
}
