package coefficients

import (
	"testing"

	mathsdk "cosmossdk.io/math"
	"github.com/productscience/inference/x/inference/types"
	"github.com/stretchr/testify/require"
)

// Coverage for previously-0% pure helpers in the dynamic-coefficient path (decode-relevant).

func TestMaxHelper(t *testing.T) {
	a := mathsdk.LegacyNewDec(3)
	b := mathsdk.LegacyNewDec(5)
	require.True(t, max(a, b).Equal(b), "a<b -> b")
	require.True(t, max(b, a).Equal(b), "a>b -> a (the larger)")
	require.True(t, max(a, a).Equal(a), "equal -> returns b")
}

func TestLegacyWeightScaleFactor(t *testing.T) {
	require.True(t, legacyWeightScaleFactor(nil).Equal(mathsdk.LegacyOneDec()),
		"nil model -> 1")
	require.True(t, legacyWeightScaleFactor(&types.PoCModelConfig{}).Equal(mathsdk.LegacyOneDec()),
		"nil WeightScaleFactor -> 1")
	require.True(t, legacyWeightScaleFactor(&types.PoCModelConfig{WeightScaleFactor: dec(2, 0)}).Equal(mathsdk.LegacyNewDec(2)),
		"explicit factor passes through")
}

func TestEffectiveForAllocation_NilParamsErrors(t *testing.T) {
	_, err := EffectiveForAllocation(nil, nil, nil, nil)
	require.Error(t, err)
}

func TestEffectiveForAllocation_Dilution(t *testing.T) {
	p := params(model("a", dec(1, 0), dec(1, 0), dec(1, 0), 10000))
	scale := &types.ConfirmationWeightScale{
		ModelId:         "a",
		BaseCoefficient: dec(1, 0),
		Config: &types.DynamicCoefficientModelConfig{
			CoeffMin:           dec(1, 0),
			CoeffMax:           dec(1, 0),
			RelativeDifficulty: dec(1, 0),
			TargetShareBps:     10000,
		},
	}
	eff, err := EffectiveForAllocation(
		p.DynamicCoefficientParams,
		[]*types.ConfirmationWeightScale{scale},
		map[string]int64{"a": 100},
		[]string{"a"},
	)
	require.NoError(t, err)
	require.Contains(t, eff, "a")
	require.False(t, eff["a"].IsNegative(), "effective coefficient is non-negative")
}
