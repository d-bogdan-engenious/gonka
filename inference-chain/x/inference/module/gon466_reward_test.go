package inference

// Makes the ECONOMIC impact executable: the reward contribution is
// ConsensusKoeff * PoC weight (delegation_weight_calculator.go:333), with no
// serving gate. So the same model-agnostic compute, relabeled from the cheap
// model to the rich one, earns strictly more -- exactly the coefficient ratio.

import (
	"testing"

	mathsdk "cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	"github.com/productscience/inference/x/inference/types"
)

func TestReward_RelabelInflatesByCoefficientRatio(t *testing.T) {
	const (
		richModel  = "aaa-rich/Model"
		cheapModel = "zzz-cheap/Model"
	)
	// Real-shaped coefficients: rich model pays 3x per unit of PoC weight.
	coeffs := map[string]mathsdk.LegacyDec{
		richModel:  mathsdk.LegacyMustNewDecFromStr("3.0"),
		cheapModel: mathsdk.LegacyMustNewDecFromStr("1.0"),
	}

	// Identical compute (PoC weight 100) — one node honestly in cheap, one relabeled to rich.
	honest := &types.ActiveParticipant{
		Index: "gonka1honest", Models: []string{cheapModel},
		MlNodes: []*types.ModelMLNodes{{MlNodes: []*types.MLNodeInfo{{NodeId: "h1", PocWeight: 100}}}},
	}
	fraud := &types.ActiveParticipant{
		Index: "gonka1fraud", Models: []string{richModel},
		MlNodes: []*types.ModelMLNodes{{MlNodes: []*types.MLNodeInfo{{NodeId: "f1", PocWeight: 100}}}},
	}

	groups := buildGroupData([]*types.ActiveParticipant{honest, fraud}, coeffs, cheapModel, mockLogger{})

	// Reward contribution = ConsensusKoeff * MemberPocWeight (the production formula).
	contrib := func(model, member string) int64 {
		g := groups[model]
		return g.ConsensusKoeff.MulInt64(g.MemberPocWeights[member]).TruncateInt64()
	}
	honestContrib := contrib(cheapModel, "gonka1honest")
	fraudContrib := contrib(richModel, "gonka1fraud")

	t.Logf("same PoC weight 100: honest(cheap) reward-weight=%d, fraud(rich) reward-weight=%d", honestContrib, fraudContrib)
	require.Equal(t, int64(100), honestContrib)
	require.Equal(t, int64(300), fraudContrib)
	require.Equal(t, int64(3), fraudContrib/honestContrib,
		"INFLATION = coefficient ratio: identical compute earns 3x by relabeling to the rich model")
}
