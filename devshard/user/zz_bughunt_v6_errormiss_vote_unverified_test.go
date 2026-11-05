package user

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"devshard/host"
	"devshard/internal/testutil"
	"devshard/signing"
	"devshard/state"
	"devshard/types"
)

// junkErrorMissVerifier stands in for a Byzantine/compromised verifier host (or a MITM on the
// verify RPC) that returns an accept vote whose signature does NOT verify for its VoterSlot.
type junkErrorMissVerifier struct{ slot uint32 }

func (junkErrorMissVerifier) VerifyTimeout(context.Context, uint64, types.TimeoutReason, *host.InferencePayload, []types.Diff, host.TimeoutArtifacts) (bool, []byte, uint32, []*types.DevshardTx, string, error) {
	return false, nil, 0, nil, "", nil
}

func (j junkErrorMissVerifier) VerifyErrorMiss(context.Context, uint64, []types.Diff, host.TimeoutArtifacts) (bool, []byte, uint32, []*types.DevshardTx, string, error) {
	return true, []byte("garbage-signature"), j.slot, nil, "", nil
}

// BUG V6-K (consensus / incentive, Byzantine-reachable): the user-side error-miss vote collector
// CollectErrorMissVotes (user/session.go:4785) appends every accept vote it receives
// UNCONDITIONALLY — there is no verifyErrorMissVote (grep confirms none exists). Its sibling, the
// timeout collector collectTimeoutVotes (session.go:4573), verifies EACH vote via verifyTimeoutVote
// (signature recovered over the vote content + VoterSlot->responder binding) BEFORE appending, with
// an explicit comment that applyTimeout is all-or-nothing so "a single spoofed slot would otherwise
// discard an honest quorum". applyErrorMiss (state/machine.go) is all-or-nothing the same way, so a
// single unverified/spoofed vote collected here poisons the whole MsgErrorMiss -> apply rejects ->
// a legitimate error-miss is suppressed: the client is never refunded and the executor escapes
// Missed. Any one verifier (or a tampered verify response) can inject it. Driven through the real
// Session.CollectErrorMissVotes with a real StateMachine for slot/weight resolution.
func TestBugHunt_V6_ErrorMissVotesCollectedWithoutVerification(t *testing.T) {
	hosts := []*signing.Secp256k1Signer{
		testutil.MustGenerateKey(t), testutil.MustGenerateKey(t), testutil.MustGenerateKey(t),
	}
	user := testutil.MustGenerateKey(t)
	group := testutil.MakeGroup(hosts)
	config := testutil.DefaultConfig(len(hosts))
	verifier := signing.NewSecp256k1Verifier()
	sm, err := state.NewStateMachine("escrow-1", config, group, 100000, user.Address(), verifier,
		testutil.MustMemoryStore(t, "escrow-1", user.Address(), config, group, 100000))
	require.NoError(t, err)
	s := &Session{sm: sm, escrowID: "escrow-1", group: group, verifierQueue: newVerifierHostQueue()}

	// Inference 1 -> executor = group[1%3] = slot 1. A verifier at the NON-executor slot 0 returns
	// an accept vote with a junk signature.
	verifiers := map[int]TimeoutVerifier{0: junkErrorMissVerifier{slot: 0}}

	votes, _, _, err := s.CollectErrorMissVotes(context.Background(), 1, verifiers, nil, host.TimeoutArtifacts{})
	require.NoError(t, err)

	require.Empty(t, votes,
		"BUG V6-K: an error-miss accept vote with an UNVERIFIED signature was collected — "+
			"CollectErrorMissVotes (session.go:4785) appends without the per-vote signature/slot check that "+
			"collectTimeoutVotes applies (verifyTimeoutVote, :4573). applyErrorMiss is all-or-nothing, so one "+
			"such vote poisons the whole MsgErrorMiss and suppresses a legitimate error-miss (client not "+
			"refunded, executor escapes Missed). Collected %d junk vote(s).", len(votes))
}
