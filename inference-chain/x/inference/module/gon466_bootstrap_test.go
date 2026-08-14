package inference

// Determines the real gate for the GON-466 bypass: how much NETWORK weight must
// back the rich model before its members can be validated (non-slot mode, as tn3:
// ValidationSlots=0, threshold vs TotalNetworkWeight). This tells us whether a
// minority set of "3 servers" can bootstrap a rich model, or whether it needs
// majority (>threshold) of the whole network.

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/productscience/inference/x/inference/types"
)

func TestBootstrap_RichModelNeedsMajorityNetworkWeight(t *testing.T) {
	const richModel = "aaa-rich/Model"
	attacker := "gonka1attacker"
	ally := "gonka1ally" // the approver whose rich voting power decides it

	key := types.PoCParticipantModelKey{ParticipantAddress: attacker, ModelID: richModel}
	const totalNetwork = int64(100)

	// ally's share of the TOTAL network weight. In ComputeModelVotingPowers, ally
	// gets this as rich voting power simply by submitting a rich commit (free, model-agnostic PoW).
	results := map[int64]bool{}
	for _, allyShare := range []int64{40, 49, 50, 51, 73} {
		wc := &PoCWeightCalculator{
			ModelVotingPowers: map[string]map[string]int64{
				// Both hold a rich commit -> both have rich voting power = their network weight.
				richModel: {ally: allyShare, attacker: 5},
			},
			TotalNetworkWeight: totalNetwork,
			PocParams:          &types.PocParams{ValidationVoteThresholdBps: 5000}, // 50%, tn3 default
			ValidationSlots:    0,                                                  // non-slot, as tn3
			Logger:             mockLogger{},
		}
		// ally validates the attacker's rich commit (cross-validation among a controlled/allied set).
		vals := []types.PoCValidationV2{
			{ParticipantAddress: attacker, ValidatorParticipantAddress: ally, ModelId: richModel, ValidatedWeight: 5},
		}
		ok := wc.pocValidated(vals, key)
		results[allyShare] = ok
		t.Logf("approver controls %d%% of network -> rich commit validated: %v", allyShare, ok)
	}

	require.False(t, results[40], "minority (40%%) must NOT be able to bootstrap the rich model")
	require.False(t, results[49], "just-under-half must NOT bootstrap")
	require.False(t, results[50], "exactly half must NOT bootstrap (strict >threshold)")
	require.True(t, results[51], "bare majority (51%%) bootstraps the rich model")
	require.True(t, results[73], "the 3-of-4 validators I control (~73%%) bootstrap it")
}
