package state

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"devshard/internal/testutil"
	"devshard/signing"
	"devshard/types"
)

// BUG V6-G (stuck challenge / lost refund): applyValidation's StatusFinished->Challenged branch
// (machine.go:1453-1463) adds the opener's weight to VotesInvalid and flips the record to
// Challenged, but never calls resolveChallengeTalliesLocked — the only code that applies
// VoteThreshold to move Challenged->Invalidated and refund the client (Balance += ActualCost,
// machine.go:1502). resolve runs only in the later StatusChallenged branch (:1477) and in
// applyValidationVote (:1581). So when a single Phase-A invalid validation already carries weight
// greater than VoteThreshold (a multi-slot validator), the record is left Challenged forever: a
// repeat MsgValidation is deduped and a MsgValidationVote returns ErrDuplicateVote, so nothing ever
// triggers resolve. The inference seals as Challenged with the client still charged — the refund is
// permanently lost. Driven through the REAL StateMachine ApplyDiff path (no mocks).
func TestBugHunt_V6_FinishedToChallengedNeverResolvesLosesRefund(t *testing.T) {
	signerX := testutil.MustGenerateKey(t)
	signerY := testutil.MustGenerateKey(t)
	user := testutil.MustGenerateKey(t)
	// X owns slots {0,1} (weight 2); Y owns slot 2. 3 slots -> VoteThreshold = 3/2 = 1.
	group := testutil.MakeMultiSlotGroup([]*signing.Secp256k1Signer{signerX, signerY}, []int{2, 1})
	config := testutil.DefaultConfig(len(group))
	verifier := signing.NewSecp256k1Verifier()
	sm, err := NewStateMachine("escrow-1", config, group, 100000, user.Address(), verifier,
		testutil.MustMemoryStore(t, "escrow-1", user.Address(), config, group, 100000))
	require.NoError(t, err)

	// Consume nonce 1 with a dangling StartInference (inference_id must == nonce) so the target
	// inference gets id=2 and its executor is the single-slot minority (slot 2 = Y), leaving X
	// (slots 0,1, weight 2 > VoteThreshold 1) as a NON-executor validator able to open a challenge.
	start1 := testutil.SignDiff(t, user, "escrow-1", 1, []*types.DevshardTx{txStart(&types.MsgStartInference{
		InferenceId: 1, PromptHash: []byte("prompt"), Model: "llama",
		InputLength: 100, MaxTokens: testutil.TestMaxTokens, StartedAt: 1000,
	})})
	_, err = sm.ApplyDiff(start1)
	require.NoError(t, err)

	// Inference 2: executor = group[2%3] = slot 2 (Y). Driven to Finished (ActualCost charged).
	applyStartConfirmFinishMultiSlot(t, sm, user, signerSlice(signerX, signerY), group, 2)
	balAfterFinish := sm.SnapshotState().Balance

	// The non-executor majority validator X (slots 0,1, weight 2) opens ONE Phase-A invalid
	// validation. VotesInvalid becomes 2 > VoteThreshold 1 — the inference is logically invalid.
	valMsg := &types.MsgValidation{InferenceId: 2, ValidatorSlot: 0, Valid: false, EscrowId: "escrow-1"}
	valMsg.ProposerSig = testutil.SignProposerTx(t, signerX, valMsg)
	nonce := sm.SnapshotState().LatestNonce + 1
	diff := testutil.SignDiff(t, user, "escrow-1", nonce, []*types.DevshardTx{txValidation(valMsg)})
	_, err = sm.ApplyDiff(diff)
	require.NoError(t, err)

	st := sm.SnapshotState()
	const execSlot = uint32(2) // Y owns slot 2 = the executor of inference 2
	var execInvalid uint32
	if hs := st.HostStats[execSlot]; hs != nil {
		execInvalid = hs.Invalid
	}

	// (a) the record should resolve to Invalidated, not sit stuck Challenged.
	assert.Equal(t, types.StatusInvalidated, st.Inferences[2].Status,
		"BUG V6-G: a single Phase-A invalid validation of weight 2 > VoteThreshold 1 left the inference "+
			"stuck StatusChallenged (VotesInvalid=%d) instead of resolving to Invalidated — the "+
			"Finished->Challenged branch (machine.go:1454) never calls resolveChallengeTalliesLocked. A "+
			"repeat MsgValidation dedups and a MsgValidationVote returns ErrDuplicateVote, so it never resolves.",
		st.Inferences[2].VotesInvalid)
	// (b) the client should be refunded ActualCost.
	assert.Greater(t, st.Balance, balAfterFinish,
		"BUG V6-G: the invalidated inference must refund ActualCost to the client, but the balance is unchanged (%d)", st.Balance)
	// (c) INCENTIVE: the executor whose work the network challenged-as-invalid must take the Invalid
	// penalty on its HostStats (which feeds the epoch reward record). Because the challenge never
	// resolves, Invalid++ (machine.go:1496) never runs — the bad executor ESCAPES the standing hit
	// AND keeps payment. This is the "power over incentives" consequence, not just lost liveness.
	assert.Equal(t, uint32(1), execInvalid,
		"BUG V6-G (incentive): the executor of the challenged-invalid inference escaped the Invalid penalty "+
			"(HostStats[%d].Invalid=%d, expected 1) and keeps payment — dodging the standing/reward hit that "+
			"should follow a bad result", execSlot, execInvalid)
}

func signerSlice(s ...*signing.Secp256k1Signer) []*signing.Secp256k1Signer { return s }
