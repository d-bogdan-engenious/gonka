package keeper_test

import (
	"testing"

	"github.com/productscience/inference/testutil"
	"github.com/productscience/inference/x/inference/types"
	"github.com/stretchr/testify/require"
)

// GON-665 QA gap-fill for PR #2206 ("challenge poc should not send cpoc commits").
//
// The eligibility gate CheckConfirmationPoCCommitEligibility is state-agnostic:
// it bars ANY same-epoch challenge target from submitting a confirmation PoC
// (CPoC) commit regardless of the challenge's state (only ch.EpochIndex ==
// activeEvent.EpochIndex matters). The shipped dev unit tests, however, only
// exercised a subset of states per path:
//   - delivery (PoCV2StoreCommit): OPEN, CHALLENGE_FAILED
//   - mempool  (CheckPoCV2StoreCommitMempool): OPEN
//
// These QA tests cover the states the dev suite left out, so the state-agnostic
// contract is proven on both paths:
//   - delivery: PASSED, ABORTED
//   - mempool : CHALLENGE_FAILED, PASSED, ABORTED
// We intentionally do not re-test the states the devs already covered.

func TestQA_PoCV2StoreCommit_ConfirmationRejectsSameEpochChallengeTarget_TerminalStates(t *testing.T) {
	for _, state := range []types.PoCChallengeState{
		types.PoCChallengeState_POC_CHALLENGE_STATE_PASSED,
		types.PoCChallengeState_POC_CHALLENGE_STATE_ABORTED,
	} {
		t.Run(state.String(), func(t *testing.T) {
			k, ctx, ms := setupEpochModelsCommitTest(t, testPoCModelID)
			require.NoError(t, k.SetPoCChallenge(ctx, types.PoCChallenge{
				EpochIndex: 1,
				Target:     testutil.Executor,
				State:      state,
			}))

			_, err := ms.PoCV2StoreCommit(ctx, confirmationCommit(makePoCV2CommitEntry(testPoCModelID, 10, 1)))
			require.ErrorIs(t, err, types.ErrPoCChallengeCPoCCommit)

			commits, getErr := k.GetAllPoCV2StoreCommitsForStage(ctx, epochModelsTrigger)
			require.NoError(t, getErr)
			require.Empty(t, commits)
		})
	}
}

func TestQA_CheckPoCV2StoreCommitMempool_ConfirmationRejectsSameEpochChallengeTarget_NonOpenStates(t *testing.T) {
	for _, state := range []types.PoCChallengeState{
		types.PoCChallengeState_POC_CHALLENGE_STATE_CHALLENGE_FAILED,
		types.PoCChallengeState_POC_CHALLENGE_STATE_PASSED,
		types.PoCChallengeState_POC_CHALLENGE_STATE_ABORTED,
	} {
		t.Run(state.String(), func(t *testing.T) {
			k, ctx, _ := setupEpochModelsCommitTest(t, testPoCModelID)
			require.NoError(t, k.SetPoCChallenge(ctx, types.PoCChallenge{
				EpochIndex: 1,
				Target:     testutil.Executor,
				State:      state,
			}))

			err := k.CheckPoCV2StoreCommitMempool(
				ctx.WithIsCheckTx(true),
				confirmationCommit(makePoCV2CommitEntry(testPoCModelID, 10, 1)),
			)
			require.ErrorIs(t, err, types.ErrPoCChallengeCPoCCommit)
		})
	}
}
