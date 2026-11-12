package inference

import (
	"context"
	"testing"

	upgradetypes "cosmossdk.io/x/upgrade/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/productscience/inference/x/inference/keeper"
	"github.com/productscience/inference/x/inference/types"
	"github.com/stretchr/testify/require"
)

// Characterization/coverage tests for the confirmation-PoC LIFECYCLE decision
// paths in confirmation_poc.go:
//   - handleConfirmationPoC          (outer entry / orchestration)
//   - checkConfirmationPoCTrigger    (trigger decision)
//   - handleConfirmationPoCPhaseTransitions (phase transitions)
//
// These assert the ACTUAL current behavior and must stay GREEN. The scoring side
// (EvaluateConfirmation/FoldEventReadings/ConfirmationScales) is covered elsewhere
// and intentionally untouched here.

// noUpgradeKeeperStub satisfies types.UpgradeKeeper with no scheduled plan, so the
// trigger path can reach HasUpgradeInWindow without a nil-interface panic. The
// minimal module-test keeper is built with a nil UpgradeKeeper, which would panic
// as soon as checkConfirmationPoCTrigger passes the active-event check.
type noUpgradeKeeperStub struct{}

func (noUpgradeKeeperStub) GetUpgradePlan(ctx context.Context) (upgradetypes.Plan, error) {
	return upgradetypes.Plan{}, nil
}

// triggerApp mirrors challengeApp (epoch 2 @ height 0, epoch 3 @ 2000, frozen
// recipe) but injects a non-nil UpgradeKeeper so the trigger path is reachable.
//
// Derived epoch-2 geometry (EpochLength=2000, default stage durations,
// ConfirmationPocSafetyWindow=50):
//   SetNewValidators      = 19
//   triggerWindowEnd      = 2000 - 0 - (10+2+2+6+1+50) = 1929
//   triggerWindowLength   = 1929 - 19 + 1 = 1911
//   InferencePhase holds for any height >= 18 inside the epoch-2 context range
//   (19 < h < 2000).
func triggerApp(t *testing.T) (AppModule, keeper.Keeper, sdk.Context) {
	t.Helper()
	k, ctx, _ := newMinimalInferenceKeeperWithStub(t)
	k.UpgradeKeeper = noUpgradeKeeperStub{}
	params, err := k.GetParams(ctx)
	require.NoError(t, err)
	params.EpochParams.EpochLength = 2000
	params.EpochParams.ConfirmationPocSafetyWindow = 50
	params.ConfirmationPocParams.AlphaThreshold = types.DecimalFromFloat(0.7)
	params.PocParams.PocV2Enabled = true
	require.NoError(t, k.SetParams(ctx, params))
	require.NoError(t, k.PrecomputeSPRTValues(ctx))
	require.NoError(t, k.SetEffectiveEpochIndex(ctx, 2))
	require.NoError(t, k.SetEpoch(ctx, &types.Epoch{Index: 2, PocStartBlockHeight: 0}))
	_, freezeErr := k.FreezePocStageRecipe(ctx, 0)
	require.NoError(t, freezeErr)
	require.NoError(t, k.SetEpoch(ctx, &types.Epoch{Index: 3, PocStartBlockHeight: 2000}))
	return NewAppModule(nil, k, nil, nil, nil, nil), k, ctx
}

func epochCtxFor(t *testing.T, k keeper.Keeper, ctx sdk.Context, epochIndex uint64) *types.EpochContext {
	t.Helper()
	params, err := k.GetParams(ctx)
	require.NoError(t, err)
	ec := types.NewEpochContext(types.Epoch{Index: epochIndex, PocStartBlockHeight: 0}, *params.EpochParams)
	return &ec
}

func TestCheckConfirmationPoCTrigger(t *testing.T) {
	tests := []struct {
		name            string
		epochIndex      uint64
		blockHeight     int64
		expected        uint64 // ExpectedConfirmationsPerEpoch
		setup           func(t *testing.T, k keeper.Keeper, ctx sdk.Context)
		wantActiveAfter bool
		verify          func(t *testing.T, k keeper.Keeper, ctx sdk.Context)
	}{
		{
			// Branch 1: EpochIndex <= 1 never triggers.
			name:        "early epoch (index 1) does not trigger",
			epochIndex:  1,
			blockHeight: 500,
			expected:    5,
		},
		{
			// Branch 2: outside InferencePhase (height 2 is PoCGenerate for epoch 2).
			name:        "non-inference phase does not trigger",
			epochIndex:  2,
			blockHeight: 2,
			expected:    5,
		},
		{
			// Branch 3: an active event already exists -> no new trigger. The
			// pre-seeded active event remains.
			name:        "active event already present does not trigger again",
			epochIndex:  2,
			blockHeight: 500,
			expected:    5,
			setup: func(t *testing.T, k keeper.Keeper, ctx sdk.Context) {
				require.NoError(t, k.SetActiveConfirmationPoCEvent(ctx, types.ConfirmationPoCEvent{
					EpochIndex:            2,
					EventSequence:         0,
					TriggerHeight:         300,
					GenerationStartHeight: 300,
					Phase:                 types.ConfirmationPoCPhase_CONFIRMATION_POC_GRACE_PERIOD,
				}))
			},
			wantActiveAfter: true,
			verify: func(t *testing.T, k keeper.Keeper, ctx sdk.Context) {
				// No per-epoch event was stored (the pre-seed only set the active
				// pointer), confirming no new event was created.
				events, err := k.GetAllConfirmationPoCEventsForEpoch(ctx, 2)
				require.NoError(t, err)
				require.Empty(t, events)
			},
		},
		{
			// Branch 4: an upgrade within the protection window blocks triggering.
			// A recent upgrade (backward check) is enough.
			name:        "upgrade in protection window does not trigger",
			epochIndex:  2,
			blockHeight: 500,
			expected:    5,
			setup: func(t *testing.T, k keeper.Keeper, ctx sdk.Context) {
				require.NoError(t, k.SetLastUpgradeHeight(ctx, 500))
			},
		},
		{
			// Branch 5a: below the valid window (height 18 < SetNewValidators 19).
			name:        "below trigger window does not trigger",
			epochIndex:  2,
			blockHeight: 18,
			expected:    5,
		},
		{
			// Branch 5b: above the valid window (height 1950 > triggerWindowEnd 1929).
			name:        "above trigger window does not trigger",
			epochIndex:  2,
			blockHeight: 1950,
			expected:    5,
		},
		{
			// Branch 7: inside the window but randFloat(0.4376 @ h=500) >=
			// triggerProbability (1/1911 ~= 0.0005).
			name:        "probability not met does not trigger",
			epochIndex:  2,
			blockHeight: 500,
			expected:    1,
		},
		{
			// Branch 8: trigger fires. ExpectedConfirmationsPerEpoch=1000 gives
			// triggerProbability ~= 0.523 > randFloat(0.4376 @ h=500), so an event
			// is created and set active.
			name:            "high probability triggers and creates event",
			epochIndex:      2,
			blockHeight:     500,
			expected:        1000,
			wantActiveAfter: true,
			verify: func(t *testing.T, k keeper.Keeper, ctx sdk.Context) {
				event, isActive, err := k.GetActiveConfirmationPoCEvent(ctx)
				require.NoError(t, err)
				require.True(t, isActive)
				require.NotNil(t, event)
				require.Equal(t, uint64(2), event.EpochIndex)
				require.Equal(t, int64(500), event.TriggerHeight)
				require.Equal(t, types.ConfirmationPoCPhase_CONFIRMATION_POC_GRACE_PERIOD, event.Phase)
				events, err := k.GetAllConfirmationPoCEventsForEpoch(ctx, 2)
				require.NoError(t, err)
				require.Len(t, events, 1)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			am, k, ctx := triggerApp(t)
			if tt.setup != nil {
				tt.setup(t, k, ctx)
			}
			ec := epochCtxFor(t, k, ctx, tt.epochIndex)
			params, err := k.GetParams(ctx)
			require.NoError(t, err)
			confParams := params.ConfirmationPocParams
			confParams.ExpectedConfirmationsPerEpoch = tt.expected

			// The error is swallowed in EndBlock by design; here we assert the
			// trigger decision completes without error.
			require.NoError(t, am.checkConfirmationPoCTrigger(
				ctx, tt.blockHeight, ec, params.EpochParams, confParams, ctx))

			_, isActive, err := k.GetActiveConfirmationPoCEvent(ctx)
			require.NoError(t, err)
			require.Equal(t, tt.wantActiveAfter, isActive)

			if tt.verify != nil {
				tt.verify(t, k, ctx)
			}
		})
	}
}

func TestHandleConfirmationPoCPhaseTransitions(t *testing.T) {
	t.Run("early epoch (index 1) is a no-op", func(t *testing.T) {
		am, k, ctx := challengeApp(t)
		ec := epochCtxFor(t, k, ctx, 1)
		params, err := k.GetParams(ctx)
		require.NoError(t, err)
		require.NoError(t, am.handleConfirmationPoCPhaseTransitions(ctx, 500, ec, params.EpochParams))
		_, isActive, err := k.GetActiveConfirmationPoCEvent(ctx)
		require.NoError(t, err)
		require.False(t, isActive)
	})

	t.Run("no active event is a no-op", func(t *testing.T) {
		am, k, ctx := challengeApp(t)
		ec := epochCtxFor(t, k, ctx, 2)
		params, err := k.GetParams(ctx)
		require.NoError(t, err)
		require.NoError(t, am.handleConfirmationPoCPhaseTransitions(ctx, 500, ec, params.EpochParams))
		_, isActive, err := k.GetActiveConfirmationPoCEvent(ctx)
		require.NoError(t, err)
		require.False(t, isActive)
	})

	t.Run("GENERATION transitions to VALIDATION", func(t *testing.T) {
		am, k, ctx := challengeApp(t)
		seedChallengeParticipant(t, k, ctx)
		ec := epochCtxFor(t, k, ctx, 2)
		params, err := k.GetParams(ctx)
		require.NoError(t, err)

		// GenerationStartHeight=180 => ValidationStart=193, ValidationEnd=198.
		require.NoError(t, k.SetActiveConfirmationPoCEvent(ctx, types.ConfirmationPoCEvent{
			EpochIndex:            2,
			EventSequence:         0,
			TriggerHeight:         180,
			GenerationStartHeight: 180,
			Phase:                 types.ConfirmationPoCPhase_CONFIRMATION_POC_GENERATION,
		}))

		require.NoError(t, am.handleConfirmationPoCPhaseTransitions(ctx, 193, ec, params.EpochParams))

		event, isActive, err := k.GetActiveConfirmationPoCEvent(ctx)
		require.NoError(t, err)
		require.True(t, isActive)
		require.Equal(t, types.ConfirmationPoCPhase_CONFIRMATION_POC_VALIDATION, event.Phase)
	})

	t.Run("COMPLETED before clear height keeps the active event", func(t *testing.T) {
		am, k, ctx := challengeApp(t)
		seedChallengeParticipant(t, k, ctx)
		ec := epochCtxFor(t, k, ctx, 2)
		params, err := k.GetParams(ctx)
		require.NoError(t, err)

		// GenerationStartHeight=180 => ValidationEnd=198, completionHeight=199,
		// clear at 199 + SetNewValidatorsDelay(1) = 200. Height 195 is before that.
		require.NoError(t, k.SetActiveConfirmationPoCEvent(ctx, types.ConfirmationPoCEvent{
			EpochIndex:            2,
			EventSequence:         0,
			TriggerHeight:         180,
			GenerationStartHeight: 180,
			Phase:                 types.ConfirmationPoCPhase_CONFIRMATION_POC_COMPLETED,
		}))

		require.NoError(t, am.handleConfirmationPoCPhaseTransitions(ctx, 195, ec, params.EpochParams))

		_, isActive, err := k.GetActiveConfirmationPoCEvent(ctx)
		require.NoError(t, err)
		require.True(t, isActive, "event must remain active before the clear height")
	})

	t.Run("COMPLETED at clear height clears the active event", func(t *testing.T) {
		am, k, ctx := challengeApp(t)
		seedChallengeParticipant(t, k, ctx)
		ec := epochCtxFor(t, k, ctx, 2)
		params, err := k.GetParams(ctx)
		require.NoError(t, err)

		require.NoError(t, k.SetActiveConfirmationPoCEvent(ctx, types.ConfirmationPoCEvent{
			EpochIndex:            2,
			EventSequence:         0,
			TriggerHeight:         180,
			GenerationStartHeight: 180,
			Phase:                 types.ConfirmationPoCPhase_CONFIRMATION_POC_COMPLETED,
		}))

		// Height 200 == completionHeight(199) + SetNewValidatorsDelay(1).
		require.NoError(t, am.handleConfirmationPoCPhaseTransitions(ctx, 200, ec, params.EpochParams))

		_, isActive, err := k.GetActiveConfirmationPoCEvent(ctx)
		require.NoError(t, err)
		require.False(t, isActive, "event must be cleared at/after the clear height")
	})
}

func TestHandleConfirmationPoC(t *testing.T) {
	t.Run("nil confirmation params returns nil", func(t *testing.T) {
		am, k, ctx := triggerApp(t)
		params, err := k.GetParams(ctx)
		require.NoError(t, err)
		params.ConfirmationPocParams = nil
		require.NoError(t, k.SetParams(ctx, params))
		require.NoError(t, am.handleConfirmationPoC(ctx, 500))
	})

	t.Run("expected confirmations zero (disabled) returns nil", func(t *testing.T) {
		// triggerApp leaves ExpectedConfirmationsPerEpoch at its default of 0.
		am, k, ctx := triggerApp(t)
		params, err := k.GetParams(ctx)
		require.NoError(t, err)
		require.Zero(t, params.ConfirmationPocParams.ExpectedConfirmationsPerEpoch)
		require.NoError(t, am.handleConfirmationPoC(ctx, 500))
		_, isActive, err := k.GetActiveConfirmationPoCEvent(ctx)
		require.NoError(t, err)
		require.False(t, isActive)
	})

	t.Run("enabled orchestration runs transitions and trigger check", func(t *testing.T) {
		am, k, ctx := triggerApp(t)
		params, err := k.GetParams(ctx)
		require.NoError(t, err)
		// Enabled but with a tiny probability (1/1911) so it does not trigger at
		// height 500 (randFloat 0.4376). Exercises the full outer orchestration:
		// phase-transition pass (no active event) + trigger check (no trigger).
		params.ConfirmationPocParams.ExpectedConfirmationsPerEpoch = 1
		require.NoError(t, k.SetParams(ctx, params))
		require.NoError(t, am.handleConfirmationPoC(ctx, 500))
		_, isActive, err := k.GetActiveConfirmationPoCEvent(ctx)
		require.NoError(t, err)
		require.False(t, isActive)
	})
}
