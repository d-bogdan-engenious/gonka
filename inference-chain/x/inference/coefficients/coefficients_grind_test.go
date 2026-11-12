package coefficients

import (
	"math/big"
	"testing"

	mathsdk "cosmossdk.io/math"
	"github.com/productscience/inference/x/inference/types"
	"github.com/stretchr/testify/require"
)

// Green characterization tests that exercise the uncovered branches of the
// dynamic-coefficient path: nil inputs, ToLegacyDec/positivity error paths,
// multi-model skips, bootstrap/clamp logic and negative-raw clamping.
//
// A *types.Decimal with |Exponent| > types.MaxDecimalExponentAbs (18) fails
// Decimal.Validate and therefore ToLegacyDec. badDec is the canonical trigger
// for every "ToLegacyDec returned an error" branch below.
func badDec() *types.Decimal { return dec(1, 19) }

func validDynParams() *types.DynamicCoefficientParams {
	return &types.DynamicCoefficientParams{
		TargetZoneBps:     500,
		StepMin:           dec(5, -3),
		StepMax:           dec(5, -2),
		BootstrapStepMax:  dec(25, -2),
		BootstrapShareBps: 100,
	}
}

func validDynConfig() *types.DynamicCoefficientModelConfig {
	return &types.DynamicCoefficientModelConfig{
		CoeffMin:           dec(5, -1),
		CoeffMax:           dec(2, 0),
		RelativeDifficulty: dec(1, 0),
		TargetShareBps:     5000,
	}
}

func validFrozenScale(id string) *types.ConfirmationWeightScale {
	return &types.ConfirmationWeightScale{ModelId: id, Config: validDynConfig()}
}

func allocScale(id string, base *types.Decimal) *types.ConfirmationWeightScale {
	return &types.ConfirmationWeightScale{
		ModelId:         id,
		BaseCoefficient: base,
		Config: &types.DynamicCoefficientModelConfig{
			CoeffMin:           dec(1, 0),
			CoeffMax:           dec(2, 0),
			RelativeDifficulty: dec(1, 0),
			TargetShareBps:     5000,
		},
	}
}

// --- positiveDecimal (nil, ToLegacyDec error, non-positive) ---

func TestPositiveDecimalBranches(t *testing.T) {
	_, err := positiveDecimal("x", nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "cannot be nil")

	_, err = positiveDecimal("x", badDec())
	require.Error(t, err)

	_, err = positiveDecimal("x", dec(0, 0))
	require.Error(t, err)
	require.Contains(t, err.Error(), "must be positive")

	got, err := positiveDecimal("x", dec(5, -1))
	require.NoError(t, err)
	require.Equal(t, "0.500000000000000000", got.String())
}

// --- encodeDecimal (zero, negative, exponent-overflow) ---

func TestEncodeDecimalBranches(t *testing.T) {
	enc, value, err := encodeDecimal(mathsdk.LegacyZeroDec())
	require.NoError(t, err)
	require.Equal(t, &types.Decimal{Value: 0, Exponent: 0}, enc)
	require.True(t, value.IsZero())

	_, _, err = encodeDecimal(mathsdk.LegacyNewDec(-1))
	require.Error(t, err)
	require.Contains(t, err.Error(), "cannot be negative")

	huge := mathsdk.LegacyNewDecFromBigInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(40), nil))
	_, _, err = encodeDecimal(huge)
	require.Error(t, err)
	require.Contains(t, err.Error(), "exceeds storage range")
}

// --- GovernanceCoefficients (nil params, model skip, legacy path, bad coeff_min) ---

func TestGovernanceCoefficientsBranches(t *testing.T) {
	require.Empty(t, GovernanceCoefficients(nil))

	// Legacy governance (no dynamic params) reads WeightScaleFactor.
	legacy := &types.PocParams{Models: []*types.PoCModelConfig{
		{ModelId: "w", WeightScaleFactor: dec(2, 0)},
	}}
	legacyCoeffs := GovernanceCoefficients(legacy)
	require.Equal(t, "2.000000000000000000", legacyCoeffs["w"].String())

	// Dynamic governance: skip nil/empty models, zero out a model whose
	// coeff_min cannot decode, and zero out a model with no scheme config.
	cfg := params(
		model("good", dec(2, 0), dec(3, 0), dec(1, 0), 10000),
		model("badmin", badDec(), dec(3, 0), dec(1, 0), 10000),
	)
	cfg.Models = append(cfg.Models,
		nil,
		&types.PoCModelConfig{ModelId: ""},
		&types.PoCModelConfig{ModelId: "noscheme", Schemes: []*types.PocSchemeParams{{
			Scheme:             types.PocScheme_POC_SCHEME_DECODE,
			DynamicCoefficient: validDynConfig(),
		}}},
	)
	coeffs := GovernanceCoefficients(cfg)
	require.Equal(t, "2.000000000000000000", coeffs["good"].String())
	require.True(t, coeffs["badmin"].IsZero())
	require.True(t, coeffs["noscheme"].IsZero())
	_, hasEmpty := coeffs[""]
	require.False(t, hasEmpty)
}

// --- Freeze (nil params, legacy skip+sort, dynamic skip+config-nil) ---

func TestFreezeBranches(t *testing.T) {
	frozen, err := Freeze(nil, nil)
	require.NoError(t, err)
	require.Nil(t, frozen.Params)
	require.Empty(t, frozen.Scales)

	// Legacy path: skip nil/empty models, sort the remaining entries.
	legacy := &types.PocParams{Models: []*types.PoCModelConfig{
		{ModelId: "zeta", WeightScaleFactor: dec(2, 0)},
		{ModelId: "alpha", WeightScaleFactor: dec(3, 0)},
		nil,
		{ModelId: ""},
	}}
	legacyFrozen, err := Freeze(legacy, nil)
	require.NoError(t, err)
	require.Nil(t, legacyFrozen.Params)
	require.Len(t, legacyFrozen.Scales, 2)
	require.Equal(t, "alpha", legacyFrozen.Scales[0].ModelId)
	require.Equal(t, "zeta", legacyFrozen.Scales[1].ModelId)

	// Dynamic path: skip nil/empty models and models whose scheme has no config.
	dyn := params(model("m", dec(1, 0), dec(2, 0), dec(1, 0), 10000))
	dyn.Models = append(dyn.Models,
		nil,
		&types.PoCModelConfig{ModelId: ""},
		&types.PoCModelConfig{ModelId: "noconf", Schemes: []*types.PocSchemeParams{{
			Scheme:             types.PocScheme_POC_SCHEME_DECODE,
			DynamicCoefficient: validDynConfig(),
		}}},
	)
	dynFrozen, err := Freeze(dyn, nil)
	require.NoError(t, err)
	require.NotNil(t, dynFrozen.Params)
	require.Len(t, dynFrozen.Scales, 1)
	require.Equal(t, "m", dynFrozen.Scales[0].ModelId)
}

// --- Calculate legacy branch (nil params): both-nil default and decode error ---

func TestCalculateLegacyBranches(t *testing.T) {
	result, err := Calculate(
		nil,
		[]*types.ConfirmationWeightScale{{ModelId: "m"}},
		nil, nil, nil,
		[]string{"m"},
		false,
	)
	require.NoError(t, err)
	require.Equal(t, "1.000000000000000000", result.Effective["m"].String())
	require.Equal(t, dec(1, 0), result.Scales[0].EffectiveCoefficient)

	_, err = Calculate(
		nil,
		[]*types.ConfirmationWeightScale{{ModelId: "m", WeightScaleFactor: badDec()}},
		nil, nil, nil,
		[]string{"m"},
		false,
	)
	require.Error(t, err)
}

// --- Calculate dynamic branch: skips nil/empty/config-nil frozen scales ---

func TestCalculateDynamicSkipsInvalidFrozenScales(t *testing.T) {
	result, err := Calculate(
		validDynParams(),
		[]*types.ConfirmationWeightScale{
			nil,
			{ModelId: ""},
			{ModelId: "x"}, // Config nil
			validFrozenScale("m"),
		},
		nil, nil,
		map[string]int64{"m": 100},
		[]string{"m"},
		false,
	)
	require.NoError(t, err)
	require.Len(t, result.Scales, 1)
	require.Equal(t, "m", result.Scales[0].ModelId)
}

// --- Calculate dynamic branch: every ToLegacyDec/positivity error return ---

func TestCalculateDynamicErrorPaths(t *testing.T) {
	t.Run("relative difficulty", func(t *testing.T) {
		scale := validFrozenScale("m")
		scale.Config.RelativeDifficulty = badDec()
		_, err := Calculate(validDynParams(),
			[]*types.ConfirmationWeightScale{scale}, nil, nil, nil, []string{"m"}, false)
		require.Error(t, err)
	})
	t.Run("step_min", func(t *testing.T) {
		p := validDynParams()
		p.StepMin = badDec()
		_, err := Calculate(p,
			[]*types.ConfirmationWeightScale{validFrozenScale("m")}, nil, nil, nil, []string{"m"}, false)
		require.Error(t, err)
	})
	t.Run("step_max", func(t *testing.T) {
		p := validDynParams()
		p.StepMax = badDec()
		_, err := Calculate(p,
			[]*types.ConfirmationWeightScale{validFrozenScale("m")}, nil, nil, nil, []string{"m"}, false)
		require.Error(t, err)
	})
	t.Run("bootstrap_step_max", func(t *testing.T) {
		p := validDynParams()
		p.BootstrapStepMax = badDec()
		_, err := Calculate(p,
			[]*types.ConfirmationWeightScale{validFrozenScale("m")}, nil, nil, nil, []string{"m"}, false)
		require.Error(t, err)
	})
	t.Run("coeff_min", func(t *testing.T) {
		scale := validFrozenScale("m")
		scale.Config.CoeffMin = badDec()
		_, err := Calculate(validDynParams(),
			[]*types.ConfirmationWeightScale{scale}, nil, nil, nil, []string{"m"}, false)
		require.Error(t, err)
	})
	t.Run("coeff_max", func(t *testing.T) {
		scale := validFrozenScale("m")
		scale.Config.CoeffMax = badDec()
		_, err := Calculate(validDynParams(),
			[]*types.ConfirmationWeightScale{scale}, nil, nil, nil, []string{"m"}, false)
		require.Error(t, err)
	})
	t.Run("initial state from previous base", func(t *testing.T) {
		previous := []*types.ConfirmationWeightScale{{ModelId: "m", BaseCoefficient: badDec()}}
		_, err := Calculate(validDynParams(),
			[]*types.ConfirmationWeightScale{validFrozenScale("m")}, previous, nil, nil, []string{"m"}, false)
		require.Error(t, err)
	})
}

// --- EffectiveForAllocation (multi-model, config-nil skip, decode errors) ---

func TestEffectiveForAllocationBranches(t *testing.T) {
	dp := validDynParams()

	t.Run("skips config-nil scale across multiple models", func(t *testing.T) {
		scales := []*types.ConfirmationWeightScale{
			{ModelId: "nocfg"}, // Config nil -> skipped in both loops
			allocScale("a", dec(1, 0)),
			allocScale("b", dec(15, -1)),
		}
		effective, err := EffectiveForAllocation(dp, scales,
			map[string]int64{"a": 100, "b": 100}, []string{"a", "b", "nocfg"})
		require.NoError(t, err)
		require.Contains(t, effective, "a")
		require.Contains(t, effective, "b")
		require.True(t, effective["nocfg"].IsZero())
		require.False(t, effective["a"].IsNegative())
	})
	t.Run("relative difficulty error", func(t *testing.T) {
		scale := allocScale("a", dec(1, 0))
		scale.Config.RelativeDifficulty = badDec()
		_, err := EffectiveForAllocation(dp,
			[]*types.ConfirmationWeightScale{scale}, nil, []string{"a"})
		require.Error(t, err)
	})
	t.Run("base coefficient error", func(t *testing.T) {
		scale := allocScale("a", badDec())
		_, err := EffectiveForAllocation(dp,
			[]*types.ConfirmationWeightScale{scale}, map[string]int64{"a": 100}, []string{"a"})
		require.Error(t, err)
	})
	t.Run("coeff_min error", func(t *testing.T) {
		scale := allocScale("a", dec(1, 0))
		scale.Config.CoeffMin = badDec()
		_, err := EffectiveForAllocation(dp,
			[]*types.ConfirmationWeightScale{scale}, map[string]int64{"a": 100}, []string{"a"})
		require.Error(t, err)
	})
}

// --- normalizedWeights clamps negative raw totals to zero ---

func TestNormalizedWeightsClampsNegativeRaw(t *testing.T) {
	difficulties := map[string]mathsdk.LegacyDec{"a": mathsdk.LegacyOneDec()}
	normalized, total := normalizedWeights(map[string]int64{"a": -50}, difficulties)
	require.True(t, normalized["a"].IsZero())
	require.True(t, total.IsZero())
}

// --- initialState error branches (bad base, bad step, out-of-range sign) ---

func TestInitialStateErrorBranches(t *testing.T) {
	config := validDynConfig()
	stepMax := mathsdk.LegacyMustNewDecFromStr("0.05")

	_, _, _, _, err := initialState("m", config,
		&types.ConfirmationWeightScale{BaseCoefficient: badDec()}, stepMax)
	require.Error(t, err)

	_, _, _, _, err = initialState("m", config,
		&types.ConfirmationWeightScale{BaseCoefficient: dec(1, 0), AdaptiveStep: badDec()}, stepMax)
	require.Error(t, err)

	_, _, _, _, err = initialState("m", config,
		&types.ConfirmationWeightScale{BaseCoefficient: dec(1, 0), AdaptiveStep: dec(25, -3), PrevSign: 5}, stepMax)
	require.Error(t, err)
	require.Contains(t, err.Error(), "must be -1, 0, or 1")
}

// --- cloneScales skips nil entries and deep-copies decimals ---

func TestCloneScalesSkipsNil(t *testing.T) {
	input := []*types.ConfirmationWeightScale{
		nil,
		{ModelId: "a", BaseCoefficient: dec(1, 0)},
	}
	output := cloneScales(input)
	require.Len(t, output, 1)
	require.Equal(t, "a", output[0].ModelId)
	require.NotSame(t, input[1].BaseCoefficient, output[0].BaseCoefficient)
	require.Equal(t, input[1].BaseCoefficient, output[0].BaseCoefficient)
}

// --- cloneParams(nil) returns nil ---

func TestCloneParamsNil(t *testing.T) {
	require.Nil(t, cloneParams(nil))
}

// --- adjust: total-zero resets share, selects bootstrap cap, direction flip ---

func TestAdjustTotalZeroBootstrapAndDirectionChange(t *testing.T) {
	base, step, sign := adjust(
		mathsdk.LegacyOneDec(),
		mathsdk.LegacyMustNewDecFromStr("0.1"),
		-1, // prevSign opposite the derived sign -> halving branch
		mathsdk.LegacyNewDec(999),
		mathsdk.LegacyZeroDec(), // total == 0 forces the bootstrap cap
		5000, 500, 100,
		mathsdk.LegacyMustNewDecFromStr("0.5"),
		mathsdk.LegacyMustNewDecFromStr("2"),
		mathsdk.LegacyMustNewDecFromStr("0.005"),
		mathsdk.LegacyMustNewDecFromStr("0.05"),
		mathsdk.LegacyMustNewDecFromStr("0.25"),
	)
	require.Equal(t, "1.050000000000000000", base.String())
	require.Equal(t, "0.050000000000000000", step.String())
	require.EqualValues(t, 1, sign)
}

// --- legacyWeightScaleFactor falls back to one when the factor cannot decode ---

func TestLegacyWeightScaleFactorBadExponent(t *testing.T) {
	got := legacyWeightScaleFactor(&types.PoCModelConfig{WeightScaleFactor: badDec()})
	require.True(t, got.Equal(mathsdk.LegacyOneDec()))
}
