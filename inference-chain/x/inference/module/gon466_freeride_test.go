package inference

// The realistic attack (no 51% required): a MINORITY fraudster free-rides on a
// model that an HONEST MAJORITY genuinely serves. The fraudster submits a
// model-agnostic PoW under the rich model's label WITHOUT serving it. The honest
// validators re-run the proof with their existing PoC check -- it passes, because
// the proof carries no model identity -- so they vote VALID and legitimize the
// fraudster themselves. No collusion; the honest majority is deceived by its own
// (passing) validation.

import (
	"context"
	"testing"

	mathsdk "cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	"github.com/productscience/inference/x/inference/types"
)

func TestFreeride_MinorityFraudsterLegitimizedByHonestMajority(t *testing.T) {
	const (
		richModel = "aaa-rich/Model" // a real, popular model the honest majority serves
		stage     = int64(100)
	)
	// Honest majority actually serving the rich model (each has a real rich commit).
	H1, H2, H3 := "gonka1honest1", "gonka1honest2", "gonka1honest3"
	honest := []string{H1, H2, H3}
	// Minority fraudster: small weight, never serves rich, only relabels a generic PoW.
	F := "gonka1fraud"

	consensusWeights := map[string]int64{H1: 100, H2: 100, H3: 100, F: 20} // total 320; honest = 300 = 94%

	// Everyone (honest + fraudster) submits a store commit under the rich model.
	// For the honest nodes it's real work; for the fraudster it's the SAME model-agnostic
	// PoW relabeled -- indistinguishable on-chain.
	all := append(append([]string{}, honest...), F)
	var storeKeys []types.PoCParticipantModelKey
	storeCommits := map[types.PoCParticipantModelKey]types.PoCV2StoreCommit{}
	dists := map[types.PoCParticipantModelKey]types.MLNodeWeightDistribution{}
	participants := map[string]types.Participant{}
	seeds := map[string]types.RandomSeed{}
	for _, a := range all {
		k := types.PoCParticipantModelKey{ParticipantAddress: a, ModelID: richModel}
		storeKeys = append(storeKeys, k)
		storeCommits[k] = types.PoCV2StoreCommit{ParticipantAddress: a, PocStageStartBlockHeight: stage, Count: 50, ModelId: richModel}
		dists[k] = types.MLNodeWeightDistribution{ParticipantAddress: a, PocStageStartBlockHeight: stage, ModelId: richModel,
			Weights: []*types.MLNodeWeight{{NodeId: a + "-n1", Weight: 50}}}
		participants[a] = types.Participant{Index: a, Address: a, ValidatorKey: a + "-vk", InferenceUrl: "u"}
		seeds[a] = types.RandomSeed{Participant: a, EpochIndex: 1, Signature: "s"}
	}

	mvp := ComputeModelVotingPowers(storeKeys, consensusWeights, nil)

	// ONLY the honest majority validates the fraudster. They vote VALID because their
	// honest PoC re-run of the model-agnostic proof passes -- they cannot tell it is
	// not real rich work. The fraudster does NOT validate anyone (pure free-rider).
	fKey := types.PoCParticipantModelKey{ParticipantAddress: F, ModelID: richModel}
	validations := map[types.PoCParticipantModelKey][]types.PoCValidationV2{}
	for _, h := range honest {
		validations[fKey] = append(validations[fKey], types.PoCValidationV2{
			ParticipantAddress: F, ValidatorParticipantAddress: h, ModelId: richModel, ValidatedWeight: 50})
	}

	wc := &PoCWeightCalculator{
		ModelVotingPowers:       mvp,
		TotalNetworkWeight:      320,
		EpochStartBlockHeight:   stage,
		TimeNormalizationFactor: mathsdk.LegacyOneDec(),
		PocParams:               &types.PocParams{ValidationVoteThresholdBps: 5000},
		ValidationSlots:         0,
		StoreCommits:            map[types.PoCParticipantModelKey]types.PoCV2StoreCommit{fKey: storeCommits[fKey]},
		NodeWeightDistributions: map[types.PoCParticipantModelKey]types.MLNodeWeightDistribution{fKey: dists[fKey]},
		Validations:             validations,
		Participants:            participants,
		Seeds:                   seeds,
		Logger:                  mockLogger{},
	}

	// Only compute the fraudster's outcome.
	aps := wc.Calculate()
	require.Len(t, aps, 1)
	require.Equal(t, F, aps[0].Index)
	require.Equal(t, []string{richModel}, aps[0].Models,
		"honest majority's valid votes legitimized the fraudster in a model it never served")
	t.Logf("fraudster %s minted under rich with weight %d, validated ONLY by honest majority", F, aps[0].Weight)

	// GON-466 seating: fraudster declares hardware = rich (free) -> stays seated.
	k := &mockKeeperForModelAssigner{
		governanceModels: []types.Model{{ProposedBy: "genesis", Id: richModel, VRam: 96}},
		hardwareNodes: map[string]*types.HardwareNodes{
			F: {Participant: F, HardwareNodes: []*types.HardwareNode{{LocalId: F + "-n1", Models: []string{richModel}}}},
		},
	}
	NewModelAssigner(k, mockLogger{}).setModelsForParticipants(context.Background(), aps, types.Epoch{Index: 2})
	require.Equal(t, []string{richModel}, aps[0].Models)
	require.Positive(t, RecalculateWeight(aps[0]),
		"FREE-RIDE: minority fraudster earns rich-model coefficient, legitimized by honest majority, serving nothing")
}
