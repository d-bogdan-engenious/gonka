package host

import (
	"testing"

	"github.com/stretchr/testify/require"

	"devshard/types"
)

// BUG V6-H (incentive / griefing, attacker-reachable): VerifyErrorMiss resolves the Finish it
// authenticates via resolveFinishMessage (host/timeout.go:470), which returns the FIRST
// inference-id match from the host's mempool and only falls back to the verified request
// `finishTx` when the mempool has NO match — never when the mempool match is junk. The mempool is
// gossip-populated through Mempool.AddTx, which does NOT verify signatures (mempool.go:120/163,
// gossip.OnTxsReceived). So any group peer can gossip a junk MsgFinishInference for the target
// inference id (correct id/escrow/slot, bad signature); resolveFinishMessage returns it over the
// genuine request artifact, VerifyErrorMiss rejects at the signature check, applyErrorMiss never
// runs, and the error-miss is never voted — the client is never refunded and the failing executor
// is never marked Missed. Driven through the REAL VerifyErrorMiss with the real StateMachine as the
// signature verifier.
func TestBugHunt_V6_ErrorMissSuppressedByGossipedJunkFinish(t *testing.T) {
	e := newErrorTimeoutEnv(t)

	// Control: the valid request Finish artifact alone proves the error-miss (accepted).
	okAlone, err := e.verify(e.finishTx, e.payload, nil)
	require.NoError(t, err)
	require.True(t, okAlone, "control: the valid request Finish proves the error-miss")

	// A peer gossips a JUNK Finish for the SAME inference id: correct id/escrow/slot so it reaches
	// the signature check, then fails ONLY on the signature. Gossip enters the mempool via AddTx
	// without signature verification.
	bad := signedErrorFinish(t, e.hosts, 1, 1, 0, e.hash)
	bad.ProposerSig = []byte("garbage")
	mempool := []*types.DevshardTx{{Tx: &types.DevshardTx_FinishInference{FinishInference: bad}}}

	// With the junk Finish in the mempool AND the valid request artifact present, the verifier must
	// STILL accept — the valid artifact proves the miss. Against current code it rejects.
	accept, err := e.verify(e.finishTx, e.payload, mempool)
	require.NoError(t, err)
	require.True(t, accept,
		"BUG V6-H: a gossiped junk Finish for the same inference id (bad signature, admitted to the "+
			"mempool unverified via AddTx) suppresses a genuine error-miss — resolveFinishMessage "+
			"(timeout.go:470) returns the mempool Finish and never falls back to the verified request "+
			"finishTx, so VerifyErrorMiss rejects and the error-miss is never voted: the client is never "+
			"refunded and the executor is never marked Missed.")
}
