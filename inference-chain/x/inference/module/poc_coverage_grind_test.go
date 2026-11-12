package inference

// Characterization tests that drive previously-uncovered branches in the
// decode/PoC weight-formation and challenge/confirmation logic. Every assertion
// pins the CURRENT behavior of the code under test; none of these are
// red/expected-to-fail. The companion harness (newMinimalInferenceKeeper,
// challengeApp, seedChallengeParticipant, noopLogger, testutil.*) lives in the
// sibling *_test.go files in this package and is reused verbatim.

import (
	"testing"

	"cosmossdk.io/collections"
	mathsdk "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/productscience/inference/testutil"
	"github.com/productscience/inference/x/inference/types"
)

// --- Pure helpers in chainvalidation.go -------------------------------------

func TestFindParticipantByAddress_Grind(t *testing.T) {
	a := &types.ActiveParticipant{Index: testutil.Executor}
	b := &types.ActiveParticipant{Index: testutil.Validator}
	list := []*types.ActiveParticipant{a, b}

	// Match on the second element exercises both the loop body and the
	// equality branch (chainvalidation.go:755-758).
	require.Same(t, b, findParticipantByAddress(list, testutil.Validator))
	require.Same(t, a, findParticipantByAddress(list, testutil.Executor))
	// No match walks the whole slice and returns nil.
	require.Nil(t, findParticipantByAddress(list, testutil.Creator))
	// Empty input returns nil without entering the loop body.
	require.Nil(t, findParticipantByAddress(nil, testutil.Executor))
}

func TestRecalculateWeight_Grind(t *testing.T) {
	p := &types.ActiveParticipant{
		MlNodes: []*types.ModelMLNodes{
			{MlNodes: []*types.MLNodeInfo{
				{NodeId: "n1", PocWeight: 10},
				{NodeId: "", PocWeight: 999}, // empty NodeId skipped (:817)
				{NodeId: "n1", PocWeight: 5},  // duplicate NodeId not re-counted
			}},
			{MlNodes: []*types.MLNodeInfo{
				{NodeId: "n2", PocWeight: 7},
			}},
		},
	}
	// 10 (n1, first) + 7 (n2); empty id and duplicate n1 contribute nothing.
	require.Equal(t, int64(17), RecalculateWeight(p))
}

func TestParticipantNodeIDs_Grind(t *testing.T) {
	participants := []*types.ActiveParticipant{
		nil, // nil participant skipped (:900)
		{
			Index: "p1",
			MlNodes: []*types.ModelMLNodes{
				nil, // nil model-nodes array skipped (:904)
				{MlNodes: []*types.MLNodeInfo{
					nil,          // nil node skipped (:908)
					{NodeId: ""}, // empty NodeId skipped (:908)
					{NodeId: "n1"},
					{NodeId: "n1"}, // idempotent set insert
				}},
			},
		},
	}

	got := participantNodeIDs(participants)
	require.Len(t, got, 1)
	require.Contains(t, got, "p1")
	require.Len(t, got["p1"], 1)
	_, ok := got["p1"]["n1"]
	require.True(t, ok)
}

func TestCalculateParticipantWeight_Grind(t *testing.T) {
	key := types.PoCParticipantModelKey{ParticipantAddress: testutil.Executor, ModelID: "m1"}
	base := func() *PoCWeightCalculator {
		return &PoCWeightCalculator{
			Logger:                  noopLogger{},
			TimeNormalizationFactor: mathsdk.LegacyOneDec(),
		}
	}

	t.Run("missing commit returns zero", func(t *testing.T) {
		wc := base()
		wc.StoreCommits = map[types.PoCParticipantModelKey]types.PoCV2StoreCommit{}
		nodes, w := wc.calculateParticipantWeight(key)
		require.Nil(t, nodes)
		require.Zero(t, w)
	})

	t.Run("zero count returns zero", func(t *testing.T) {
		wc := base()
		wc.StoreCommits = map[types.PoCParticipantModelKey]types.PoCV2StoreCommit{key: {Count: 0}}
		nodes, w := wc.calculateParticipantWeight(key)
		require.Nil(t, nodes)
		require.Zero(t, w)
	})

	t.Run("missing distribution returns zero", func(t *testing.T) {
		wc := base()
		wc.StoreCommits = map[types.PoCParticipantModelKey]types.PoCV2StoreCommit{key: {Count: 10}}
		wc.NodeWeightDistributions = map[types.PoCParticipantModelKey]types.MLNodeWeightDistribution{}
		nodes, w := wc.calculateParticipantWeight(key)
		require.Nil(t, nodes)
		require.Zero(t, w)
	})

	t.Run("multiple nodes are sorted by id", func(t *testing.T) {
		wc := base()
		wc.StoreCommits = map[types.PoCParticipantModelKey]types.PoCV2StoreCommit{key: {Count: 30}}
		wc.NodeWeightDistributions = map[types.PoCParticipantModelKey]types.MLNodeWeightDistribution{
			key: {Weights: []*types.MLNodeWeight{
				{NodeId: "z", Weight: 20},
				{NodeId: "a", Weight: 10},
			}},
		}
		nodes, w := wc.calculateParticipantWeight(key)
		require.Equal(t, int64(30), w) // raw count * 1.0 normalization
		require.Len(t, nodes, 2)
		// slices.SortFunc comparator runs only with >=2 nodes (:478-480).
		require.Equal(t, "a", nodes[0].nodeId)
		require.Equal(t, int64(10), nodes[0].weight)
		require.Equal(t, "z", nodes[1].nodeId)
		require.Equal(t, int64(20), nodes[1].weight)
	})
}

// --- validatedParticipant rejection branches --------------------------------

func TestValidatedParticipant_Grind_RejectionBranches(t *testing.T) {
	key := types.PoCParticipantModelKey{ParticipantAddress: testutil.Executor, ModelID: "m1"}

	t.Run("participant not found", func(t *testing.T) {
		wc := &PoCWeightCalculator{
			Participants: map[string]types.Participant{},
			Logger:       noopLogger{},
		}
		require.Nil(t, wc.validatedParticipant(key)) // :175
	})

	t.Run("no validations submitted", func(t *testing.T) {
		wc := &PoCWeightCalculator{
			Participants: map[string]types.Participant{
				testutil.Executor: {Address: testutil.Executor, ValidatorKey: "vk"},
			},
			Validations: map[types.PoCParticipantModelKey][]types.PoCValidationV2{},
			Logger:      noopLogger{},
		}
		require.Nil(t, wc.validatedParticipant(key)) // :183
	})

	t.Run("empty validator key", func(t *testing.T) {
		wc := &PoCWeightCalculator{
			TimeNormalizationFactor: mathsdk.LegacyOneDec(),
			Participants: map[string]types.Participant{
				testutil.Executor: {Address: testutil.Executor, ValidatorKey: ""},
			},
			StoreCommits: map[types.PoCParticipantModelKey]types.PoCV2StoreCommit{
				key: {Count: 10},
			},
			NodeWeightDistributions: map[types.PoCParticipantModelKey]types.MLNodeWeightDistribution{
				key: {Weights: []*types.MLNodeWeight{{NodeId: "n1", Weight: 10}}},
			},
			Validations: map[types.PoCParticipantModelKey][]types.PoCValidationV2{
				key: {{ValidatorParticipantAddress: testutil.Validator, ValidatedWeight: 1}},
			},
			Logger: noopLogger{},
		}
		require.Nil(t, wc.validatedParticipant(key)) // :205
	})

	t.Run("seed not found after passing validation", func(t *testing.T) {
		wc := &PoCWeightCalculator{
			TimeNormalizationFactor: mathsdk.LegacyOneDec(),
			Participants: map[string]types.Participant{
				testutil.Executor: {Address: testutil.Executor, ValidatorKey: "vk"},
			},
			StoreCommits: map[types.PoCParticipantModelKey]types.PoCV2StoreCommit{
				key: {Count: 10},
			},
			NodeWeightDistributions: map[types.PoCParticipantModelKey]types.MLNodeWeightDistribution{
				key: {Weights: []*types.MLNodeWeight{{NodeId: "n1", Weight: 10}}},
			},
			Validations: map[types.PoCParticipantModelKey][]types.PoCValidationV2{
				key: {{ValidatorParticipantAddress: testutil.Validator, ValidatedWeight: 1}},
			},
			ModelVotingPowers:  map[string]map[string]int64{"m1": {testutil.Validator: 100}},
			TotalNetworkWeight: 100,
			Seeds:              map[string]types.RandomSeed{}, // absent -> :215
			PocParams:          &types.PocParams{},
			Logger:             noopLogger{},
		}
		require.Nil(t, wc.validatedParticipant(key)) // :215
	})
}

// --- NewPoCWeightCalculator + slot-sampling pocValidated branches -----------

// newSlotCalc builds a calculator via the real constructor with slot sampling
// enabled, which populates the cached sortedVotingPowers map
// (NewPoCWeightCalculator :102-107).
func newSlotCalc(modelVP map[string]int64) *PoCWeightCalculator {
	return NewPoCWeightCalculator(
		map[string]map[string]int64{"m1": modelVP},
		100,
		nil,
		nil,
		nil,
		&types.PocParams{},
		nil,
		nil,
		0,
		noopLogger{},
		mathsdk.LegacyOneDec(),
		false,
		nil,
		"grind-hash",
		128, // validationSlots > 0
	)
}

func TestPocValidated_Grind_SlotInvalidMajorityRejects(t *testing.T) {
	key := types.PoCParticipantModelKey{ParticipantAddress: testutil.Executor, ModelID: "m1"}
	// A single validator owns the full network weight so it receives every
	// sampled slot. Its invalid vote (<=0) drives invalidSlots to the full slot
	// count and trips the invalid-majority rejection (:315, :325).
	wc := newSlotCalc(map[string]int64{testutil.Validator: 100})
	ok := wc.pocValidated([]types.PoCValidationV2{
		{ValidatorParticipantAddress: testutil.Validator, ValidatedWeight: 0},
	}, key)
	require.False(t, ok)
}

func TestPocValidated_Grind_SlotAssignedValidatorWithoutVoteAbstains(t *testing.T) {
	key := types.PoCParticipantModelKey{ParticipantAddress: testutil.Executor, ModelID: "m1"}
	// Slots are assigned to Validator (the only address with voting power), but
	// the only submitted vote comes from Creator, who holds no slots. Every
	// assigned slot therefore has no matching vote and abstains (:310). With no
	// majority and no guardians the participant is rejected.
	wc := newSlotCalc(map[string]int64{testutil.Validator: 100})
	ok := wc.pocValidated([]types.PoCValidationV2{
		{ValidatorParticipantAddress: testutil.Creator, ValidatedWeight: 1},
	}, key)
	require.False(t, ok)
}

// --- guardianProtection split branch ----------------------------------------

func TestGuardianProtection_Grind_SplitRejects(t *testing.T) {
	key := types.PoCParticipantModelKey{ParticipantAddress: testutil.Executor, ModelID: "m1"}
	wc := &PoCWeightCalculator{
		GuardianEnabled:   true,
		GuardianAddresses: map[string]bool{testutil.Validator: true, testutil.Validator2: true},
		Validations: map[types.PoCParticipantModelKey][]types.PoCValidationV2{
			key: {
				{ValidatorParticipantAddress: testutil.Validator, ValidatedWeight: 1},   // valid
				{ValidatorParticipantAddress: testutil.Validator2, ValidatedWeight: -1}, // invalid
			},
		},
		Logger: noopLogger{},
	}
	// Guardians disagree -> no unanimous decision -> reject (:434).
	require.False(t, wc.guardianProtection(key, ValidationOutcome{TotalWeight: 100}))
}

// --- computeRatio -----------------------------------------------------------

func TestComputeRatio_Grind(t *testing.T) {
	// totalExpected == 0 returns a flat ratio of 1 (:568).
	require.Equal(t, "1", computeRatio(5, 0).ToDecimal().String())
	// reading == totalExpected divides by the 0.909 deviation coefficient,
	// exceeds 1, and is clamped back to 1.
	require.Equal(t, "1", computeRatio(1000, 1000).ToDecimal().String())
	// A partial reading yields a fraction strictly below 1.
	partial := computeRatio(454, 1000)
	require.NotEqual(t, "1", partial.ToDecimal().String())
	require.Greater(t, partial.ToFloat(), 0.0)
	require.Less(t, partial.ToFloat(), 1.0)
}

// --- stripChallengeSkipFromPreserved extra branches -------------------------

func TestStripChallengeSkipFromPreserved_Grind(t *testing.T) {
	snap := types.PreservedNodesSnapshot{
		ModelPreservedNodes: []*types.ModelPreservedNodes{
			nil, // nil model entry skipped (:326)
			{
				ModelId: "m1",
				Participants: []*types.ParticipantPreservedNodes{
					nil, // nil participant skipped (:331-332)
					{ParticipantId: testutil.Executor},
					{ParticipantId: testutil.Validator},
				},
			},
		},
	}

	// Empty skip set short-circuits and returns the snapshot unchanged (:322).
	unchanged := stripChallengeSkipFromPreserved(snap, nil)
	require.Len(t, unchanged.ModelPreservedNodes[1].Participants, 3)

	got := stripChallengeSkipFromPreserved(snap, map[string]struct{}{testutil.Executor: {}})
	require.Len(t, got.ModelPreservedNodes[1].Participants, 1)
	require.Equal(t, testutil.Validator, got.ModelPreservedNodes[1].Participants[0].ParticipantId)
}

// --- distributionsForAdmittedKeys (keeper-backed) ---------------------------

func TestDistributionsForAdmittedKeys_Grind(t *testing.T) {
	k, ctx := newMinimalInferenceKeeper(t)
	am := NewAppModule(nil, k, nil, nil, nil, nil)

	missing := types.PoCParticipantModelKey{ParticipantAddress: testutil.Executor, ModelID: "m1"}
	present := types.PoCParticipantModelKey{ParticipantAddress: testutil.Validator, ModelID: "m1"}

	require.NoError(t, k.SetMLNodeWeightDistribution(ctx, types.MLNodeWeightDistribution{
		ParticipantAddress:       testutil.Validator,
		PocStageStartBlockHeight: 100,
		ModelId:                  "m1",
		Weights:                  []*types.MLNodeWeight{{NodeId: "n1", Weight: 5}},
	}))

	// The missing key is silently skipped (collections.ErrNotFound, :1212); the
	// present key is loaded into the result (:1218).
	res, err := am.distributionsForAdmittedKeys(ctx, 100, []types.PoCParticipantModelKey{missing, present})
	require.NoError(t, err)
	require.Len(t, res, 1)
	require.Contains(t, res, present)
	require.NotContains(t, res, missing)
}

// --- getInferenceServingNodeIds (keeper-backed) -----------------------------

func TestGetInferenceServingNodeIds_Grind(t *testing.T) {
	k, ctx := newMinimalInferenceKeeper(t)
	am := NewAppModule(nil, k, nil, nil, nil, nil)

	// Epoch index <= 1 returns an empty map without touching the store (:852).
	require.Empty(t, am.getInferenceServingNodeIds(ctx, types.Epoch{Index: 1}))

	// Note: a nil *ParticipantPreservedNodes cannot be persisted (proto marshal
	// panics on nil slice elements), so the nil-participant guard at :868 is
	// defensive and unreachable through the real keeper store path.
	require.NoError(t, k.SetPreservedNodesSnapshot(ctx, types.PreservedNodesSnapshot{
		ModelPreservedNodes: []*types.ModelPreservedNodes{{
			ModelId: "m1",
			Participants: []*types.ParticipantPreservedNodes{
				{ParticipantId: testutil.Executor, NodeIds: []string{"n1", "n1", "n2"}},
			},
		}},
	}))

	got := am.getInferenceServingNodeIds(ctx, types.Epoch{Index: 2})
	require.Len(t, got, 1)
	require.Contains(t, got, testutil.Executor)
	require.Len(t, got[testutil.Executor], 2) // n1 deduped, n2 added
	require.Contains(t, got[testutil.Executor], "n1")
	require.Contains(t, got[testutil.Executor], "n2")
}

// --- GetPreservedNodesByParticipant (keeper-backed) -------------------------

func TestGetPreservedNodesByParticipant_Grind(t *testing.T) {
	k, ctx := newMinimalInferenceKeeper(t)
	am := NewAppModule(nil, k, nil, nil, nil, nil)

	// nil snapshot returns an empty map (:700).
	res, err := am.GetPreservedNodesByParticipant(ctx, 2, nil)
	require.NoError(t, err)
	require.Empty(t, res)

	snapshot := types.PreservedNodesSnapshot{
		ModelPreservedNodes: []*types.ModelPreservedNodes{
			{ModelId: "m-empty", Participants: nil}, // no participants -> skipped (:705)
			{ModelId: "m-missing", Participants: []*types.ParticipantPreservedNodes{
				{ParticipantId: testutil.Executor, NodeIds: []string{"n1"}},
			}}, // no subgroup data -> skipped (:710)
			{ModelId: "m1", Participants: []*types.ParticipantPreservedNodes{
				nil, // nil participant skipped (:719)
				{ParticipantId: testutil.Executor, NodeIds: []string{"n1"}},
			}},
		},
	}

	// Only m1 has subgroup data. Its validation weight lists a preserved node
	// (n1) and a non-preserved node (n2, :735). A nil MLNode (:731) cannot be
	// persisted (proto marshal panics), so that guard is defensive.
	k.SetEpochGroupData(ctx, types.EpochGroupData{
		EpochIndex: 2,
		ModelId:    "m1",
		ValidationWeights: []*types.ValidationWeight{{
			MemberAddress: testutil.Executor,
			MlNodes: []*types.MLNodeInfo{
				{NodeId: "n1"},
				{NodeId: "n2"},
			},
		}},
	})

	res, err = am.GetPreservedNodesByParticipant(ctx, 2, &snapshot)
	require.NoError(t, err)
	require.Len(t, res, 1)
	require.Len(t, res[testutil.Executor], 1)
	require.Len(t, res[testutil.Executor]["m1"], 1)
	require.Equal(t, "n1", res[testutil.Executor]["m1"][0].NodeId)
}

// --- poc_challenge decision helpers (keeper-backed) -------------------------

func TestCanRotateChallenge_Grind_ErrorReturnsFalse(t *testing.T) {
	am, _, ctx := challengeApp(t)
	// Epoch 99 has no stored record, so ChallengeSafetyFinish errors and
	// canRotateChallenge returns false (:109).
	require.False(t, am.canRotateChallenge(ctx, types.PoCChallenge{EpochIndex: 99, Target: testutil.Executor}))
}

func TestAdvanceAfterDecision_Grind_RotateSafetyError(t *testing.T) {
	am, _, ctx := challengeApp(t)
	// rotate=true but the epoch is unknown: canRotateChallenge fails, then the
	// fall-through ChallengeSafetyFinish fails too, surfacing the error (:126).
	err := am.advanceAfterDecision(ctx, types.PoCChallenge{EpochIndex: 99, Target: testutil.Executor}, true)
	require.Error(t, err)
}

func TestDecideCurrentChallengeSegments_Grind_SkipsNonOpenChallenge(t *testing.T) {
	am, k, ctx := challengeApp(t)
	require.NoError(t, k.SetPoCChallenge(ctx, types.PoCChallenge{
		EpochIndex:  2,
		Target:      testutil.Executor,
		StartHeight: 100,
		State:       types.PoCChallengeState_POC_CHALLENGE_STATE_PASSED,
	}))
	// Epoch matches but the record is not OPEN, so it is skipped (:49) and left
	// untouched.
	require.NoError(t, am.decideCurrentChallengeSegments(ctx, 2, 500, 180, false))
	ch, found, err := k.GetPoCChallenge(ctx, testutil.Executor)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, types.PoCChallengeState_POC_CHALLENGE_STATE_PASSED, ch.State)
}

func TestEvaluatePunishableChallengeSegment_Grind_EarlyErrors(t *testing.T) {
	scaleEGD := func(epoch uint64) types.EpochGroupData {
		return types.EpochGroupData{
			EpochIndex: epoch,
			ConfirmationWeightScales: []*types.ConfirmationWeightScale{
				{ModelId: "m1", WeightScaleFactor: types.DecimalFromFloat(1)},
			},
		}
	}

	t.Run("no frozen recipe for epoch", func(t *testing.T) {
		am, _, ctx := challengeApp(t)
		err := am.evaluatePunishableChallengeSegment(ctx,
			types.PoCChallenge{EpochIndex: 7, Target: testutil.Executor, StartHeight: 100}, 500, 180)
		require.Error(t, err) // :155
	})

	t.Run("missing epoch group data", func(t *testing.T) {
		am, _, ctx := challengeApp(t)
		err := am.evaluatePunishableChallengeSegment(ctx,
			types.PoCChallenge{EpochIndex: 2, Target: testutil.Executor, StartHeight: 100}, 500, 180)
		require.Error(t, err) // :160 (recipe exists for epoch 2, no group data)
	})

	t.Run("no confirmation weight scales", func(t *testing.T) {
		am, k, ctx := challengeApp(t)
		k.SetEpochGroupData(ctx, types.EpochGroupData{EpochIndex: 2})
		err := am.evaluatePunishableChallengeSegment(ctx,
			types.PoCChallenge{EpochIndex: 2, Target: testutil.Executor, StartHeight: 100}, 500, 180)
		require.Error(t, err) // :164
	})

	t.Run("validation snapshot missing", func(t *testing.T) {
		am, k, ctx := challengeApp(t)
		k.SetEpochGroupData(ctx, scaleEGD(2))
		err := am.evaluatePunishableChallengeSegment(ctx,
			types.PoCChallenge{EpochIndex: 2, Target: testutil.Executor, StartHeight: 100}, 500, 8888)
		require.Error(t, err) // :175 (no snapshot at height 8888)
	})
}

func TestChallengeCalculatorResult_Grind_ParticipantNotFound(t *testing.T) {
	am, k, ctx := challengeApp(t)
	target := sdk.MustAccAddressFromBech32(testutil.Executor)
	require.NoError(t, k.PoCChallengeCommits.Set(ctx, collections.Join(target, "m1"), types.PoCV2StoreCommit{
		ParticipantAddress: testutil.Executor,
		ModelId:            "m1",
		Count:              5,
	}))
	params, err := k.GetParams(ctx)
	require.NoError(t, err)
	// A commit exists so the empty-input short-circuit is bypassed, but the
	// target is not a registered participant (:263).
	_, err = am.challengeCalculatorResult(ctx,
		types.PoCChallenge{EpochIndex: 2, Target: testutil.Executor, StartHeight: 100},
		500, types.PoCValidationSnapshot{}, params)
	require.Error(t, err)
}

// --- evaluateConfirmation early returns (keeper-backed) ---------------------

func TestEvaluateConfirmation_Grind_EarlyReturns(t *testing.T) {
	scales := []*types.ConfirmationWeightScale{
		{ModelId: "m1", WeightScaleFactor: types.DecimalFromFloat(1)},
	}

	t.Run("tracking recipe skips haircut", func(t *testing.T) {
		k, ctx := newMinimalInferenceKeeper(t)
		am := NewAppModule(nil, k, nil, nil, nil, nil)
		event := &types.ConfirmationPoCEvent{
			EpochIndex: 2, TriggerHeight: 100,
			Recipe: &types.PocStageRecipe{Tracking: true},
		}
		egd := &types.EpochGroupData{EpochIndex: 2}
		require.NoError(t, am.evaluateConfirmation(ctx, event, egd)) // :432
	})

	t.Run("no confirmation weight scales", func(t *testing.T) {
		k, ctx := newMinimalInferenceKeeper(t)
		am := NewAppModule(nil, k, nil, nil, nil, nil)
		event := &types.ConfirmationPoCEvent{
			EpochIndex: 2, TriggerHeight: 100,
			Recipe: &types.PocStageRecipe{},
		}
		egd := &types.EpochGroupData{EpochIndex: 2}
		require.NoError(t, am.evaluateConfirmation(ctx, event, egd)) // :441
	})

	t.Run("validation snapshot missing", func(t *testing.T) {
		k, ctx := newMinimalInferenceKeeper(t)
		am := NewAppModule(nil, k, nil, nil, nil, nil)
		event := &types.ConfirmationPoCEvent{
			EpochIndex: 2, TriggerHeight: 7777,
			Recipe: &types.PocStageRecipe{},
		}
		egd := &types.EpochGroupData{EpochIndex: 2, ConfirmationWeightScales: scales}
		require.NoError(t, am.evaluateConfirmation(ctx, event, egd)) // :451
	})

	t.Run("snapshot has no confirmation models", func(t *testing.T) {
		k, ctx := newMinimalInferenceKeeper(t)
		am := NewAppModule(nil, k, nil, nil, nil, nil)
		require.NoError(t, k.SetPoCValidationSnapshot(ctx, types.PoCValidationSnapshot{
			PocStageStartHeight: 600,
			ModelVotingPowers: []*types.ModelVotingPowers{{
				ModelId:      "other-model",
				VotingPowers: []*types.VotingPowerEntry{{Address: testutil.Validator, VotingPower: 100}},
			}},
		}))
		event := &types.ConfirmationPoCEvent{
			EpochIndex: 2, TriggerHeight: 600,
			Recipe: &types.PocStageRecipe{},
		}
		egd := &types.EpochGroupData{EpochIndex: 2, ConfirmationWeightScales: scales}
		require.NoError(t, am.evaluateConfirmation(ctx, event, egd)) // :458
	})

	t.Run("active participants not found", func(t *testing.T) {
		k, ctx := newMinimalInferenceKeeper(t)
		am := NewAppModule(nil, k, nil, nil, nil, nil)
		require.NoError(t, k.SetPoCValidationSnapshot(ctx, types.PoCValidationSnapshot{
			PocStageStartHeight: 800,
			ModelVotingPowers: []*types.ModelVotingPowers{{
				ModelId:      "m1",
				VotingPowers: []*types.VotingPowerEntry{{Address: testutil.Validator, VotingPower: 100}},
			}},
		}))
		event := &types.ConfirmationPoCEvent{
			EpochIndex: 9, TriggerHeight: 800,
			Recipe: &types.PocStageRecipe{},
		}
		egd := &types.EpochGroupData{EpochIndex: 9, ConfirmationWeightScales: scales}
		require.Error(t, am.evaluateConfirmation(ctx, event, egd)) // :473
	})
}
