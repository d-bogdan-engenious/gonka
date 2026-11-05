package state

import (
	"testing"

	"github.com/stretchr/testify/require"

	"devshard/internal/testutil"
	"devshard/signing"
	"devshard/types"
)

// BUG V6-D (consensus integrity): the self-validation guard is slot-level, not address-level.
// applyValidation rejects a validation only when ValidatorSlot == ExecutorSlot (machine.go:1414),
// and applyValidationVote (machine.go:1531-1584) has NO executor check at all. The executor is a
// single slot (group[inferenceID % len(group)]), but a validator ADDRESS can own several slots —
// so a multi-slot executor can vote on its OWN inference from a sibling slot it also owns. Its full
// address weight (addressToSlotCount, including the executor slot) is tallied, letting it single-
// handedly rescue its own provably-challenged inference to StatusValidated and get paid for bad
// work. Driven through the REAL StateMachine via ApplyDiff with real proposer/diff signatures — the
// actual consensus apply path, no mocks — exactly like the passing TestApplyDiff_ValidationVote_
// MultiSlotWeight, but with the executor casting the ballot.
func TestBugHunt_V6_ExecutorSelfValidatesViaSiblingSlot(t *testing.T) {
	signers := []*signing.Secp256k1Signer{
		testutil.MustGenerateKey(t), testutil.MustGenerateKey(t), testutil.MustGenerateKey(t),
	}
	user := testutil.MustGenerateKey(t)
	// signer0 owns slots {0,1,2}; signer1 slot 3; signer2 slot 4. 5 slots, VoteThreshold = 5/2 = 2.
	group := testutil.MakeMultiSlotGroup(signers, []int{3, 1, 1})
	config := testutil.DefaultConfig(len(group))
	verifier := signing.NewSecp256k1Verifier()
	sm, err := NewStateMachine("escrow-1", config, group, 10000, user.Address(), verifier,
		testutil.MustMemoryStore(t, "escrow-1", user.Address(), config, group, 10000))
	require.NoError(t, err)

	// Inference 1: executor slot = group[1%5].SlotID = slot 1, owned by signer0 (a MULTI-slot address).
	applyStartConfirmFinishMultiSlot(t, sm, user, signers, group, 1)

	// An honest validator (signer1, slot 3) challenges the inference as invalid.
	valMsg := &types.MsgValidation{InferenceId: 1, ValidatorSlot: 3, Valid: false, EscrowId: "escrow-1"}
	valMsg.ProposerSig = testutil.SignProposerTx(t, signers[1], valMsg)
	nonce := sm.SnapshotState().LatestNonce + 1
	diff := testutil.SignDiff(t, user, "escrow-1", nonce, []*types.DevshardTx{txValidation(valMsg)})
	_, err = sm.ApplyDiff(diff)
	require.NoError(t, err)
	require.Equal(t, types.StatusChallenged, sm.SnapshotState().Inferences[1].Status,
		"precondition: the honest challenge opened the record")

	// The EXECUTOR (signer0, which owns the executor slot 1) votes VALID on its OWN inference from a
	// sibling slot it also owns (slot 0, != executor slot 1). This must be rejected as self-validation.
	vote := &types.MsgValidationVote{InferenceId: 1, VoterSlot: 0, VoteValid: true, EscrowId: "escrow-1"}
	vote.ProposerSig = testutil.SignProposerTx(t, signers[0], vote)
	nonce = sm.SnapshotState().LatestNonce + 1
	diff = testutil.SignDiff(t, user, "escrow-1", nonce, []*types.DevshardTx{txVote(vote)})
	_, err = sm.ApplyDiff(diff)

	st := sm.SnapshotState()
	require.NotEqual(t, types.StatusValidated, st.Inferences[1].Status,
		"BUG V6-D: the executor (signer0, owns executor slot 1) validated its OWN challenged inference "+
			"by voting VALID from sibling slot 0 — the self-validation guard is slot-level not address-level "+
			"(machine.go:1414 is slot-equality, applyValidationVote has no executor check). "+
			"VotesValid=%d VotesInvalid=%d err=%v",
		st.Inferences[1].VotesValid, st.Inferences[1].VotesInvalid, err)
}
