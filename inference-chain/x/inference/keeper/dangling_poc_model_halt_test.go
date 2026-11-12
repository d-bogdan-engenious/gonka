package keeper_test

import (
	"testing"

	"github.com/productscience/inference/x/inference/types"
	"github.com/stretchr/testify/require"
)

// Consensus-halt audit (class 1 — error returned from EndBlocker):
//
// Epoch formation reads the LIVE PoC config, not a snapshot. In
// module.go onEndOfPoCValidationStage -> initializeUpcomingEpochModelGroups
// the chain iterates params.PocParams.GetModelConfigs() and, for each, calls
// GetGovernanceModel(modelId). A model present in PocParams.ModelConfigs but
// missing from the governance model store makes that helper return
//     fmt.Errorf("poc model %q missing from governance models")
// which propagates: initializeUpcomingEpochModelGroups (module.go:555) ->
// onEndOfPoCValidationStage -> EndBlock (module.go:497 "return err") -> the
// SDK aborts the block => deterministic CHAIN HALT at the epoch boundary.
//
// MsgDeleteGovernanceModel (msg_server_delete_governance_model.go) checks only
// permission + model-exists, then deletes. It does NOT check whether the model
// is still referenced by PocParams.ModelConfigs, and it does NOT scrub that
// reference. Params.Validate()/PocParams.Validate() are pure type-level funcs
// that cannot see the governance-model store, so they cannot catch it either.
// => a single governance delete (or a MsgUpdateParams that adds an
// unregistered model) silently arms a halt that detonates at the next epoch.
//
// The existing TestMsgServer_DeleteGovernanceModel_DoesNotAffectCurrentEpochSnapshot
// proves the CURRENT epoch is safe (it uses ModelSnapshot). This asserts the
// complementary invariant the UPCOMING epoch needs, which currently fails.
//
// RED on the current tree; GREEN once the delete is rejected while referenced
// (or scrubs the PocParams reference), or formation skips-with-log instead of
// erroring.
func TestDeleteGovernanceModel_DanglingPocParamsRef_ArmsEpochFormationHalt(t *testing.T) {
	k, ms, ctx := setupMsgServer(t)

	const modelID = "dangling-poc-model"

	// Register it as a governance model.
	k.SetModel(ctx, &types.Model{
		ProposedBy:             k.GetAuthority(),
		Id:                     modelID,
		UnitsOfComputePerToken: 1,
	})
	if _, found := k.GetGovernanceModel(ctx, modelID); !found {
		t.Fatalf("precondition: model %q should be a governance model", modelID)
	}

	// Reference it in the LIVE PoC params that epoch formation reads.
	params, err := k.GetParams(ctx)
	require.NoError(t, err)
	if params.PocParams == nil {
		params.PocParams = types.DefaultParams().PocParams
	}
	params.PocParams.Models = append(params.PocParams.Models, &types.PoCModelConfig{ModelId: modelID})
	require.NoError(t, k.SetParams(ctx, params))

	// Governance deletes the model. Today this succeeds unconditionally.
	_, err = ms.DeleteGovernanceModel(ctx, &types.MsgDeleteGovernanceModel{
		Authority: k.GetAuthority(),
		Id:        modelID,
	})
	require.NoError(t, err, "delete currently succeeds with no referential guard")

	// Reconstruct the exact predicate initializeUpcomingEpochModelGroups will hit
	// at the next epoch boundary: a model still listed in PocParams whose
	// governance entry is gone.
	gotParams, err := k.GetParams(ctx)
	require.NoError(t, err)
	stillReferenced := false
	for _, mc := range gotParams.PocParams.GetModelConfigs() {
		if mc != nil && mc.ModelId == modelID {
			stillReferenced = true
			break
		}
	}
	_, govFound := k.GetGovernanceModel(ctx, modelID)

	// Safe invariant for the UPCOMING epoch: a PoC-referenced model must still
	// resolve in governance (else formation errors -> EndBlock halt). This
	// currently fails -> RED, reproducing the armed halt.
	require.Falsef(t, stillReferenced && !govFound,
		"dangling PoC model config after governance delete: model %q is still in "+
			"PocParams.ModelConfigs but gone from governance models -> "+
			"initializeUpcomingEpochModelGroups returns an error that propagates to "+
			"EndBlock and halts the chain at the next epoch boundary. The delete must "+
			"be rejected while referenced (or scrub the reference), or formation must "+
			"skip-with-log instead of returning an error.", modelID)
}
