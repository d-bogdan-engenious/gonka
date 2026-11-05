package host

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"devshard/internal/testutil"
	"devshard/types"
)

// BUG V6-J (DoS / griefing, Byzantine-reachable): Mempool.HasFinish (host/mempool.go:107) counts
// peer-gossiped Finishes, UNLIKE its siblings StaleFinishes (mempool.go:86) and QueuedConfirmStart
// (mempool.go:130), which both skip ProposedAt==0 peer-imported entries with the rationale "AddTx
// does not verify signatures, so a gossiped copy must not stand in for this host's own receipt".
// The host's own Finish is queued with ProposedAt>0; a gossiped one arrives via gossip.OnTxsReceived
// -> Mempool.AddTx with ProposedAt=0 and NO signature check. At the StatusPending executor path
// (host.go:1155) `h.storedResponse != nil && h.mempool.HasFinish(id)` makes the executor return NO
// ExecutionJob and replay from storage instead; with no stored body (it never actually ran) the
// request yields ReasonInferenceDisappeared. So one junk Finish gossiped by any group member makes
// the real executor SKIP its first execution of that inference -> it never runs, stalls to a
// timeout/miss (the victim executor takes the blame, the user gets no answer), and the junk tx is
// never removed (RemoveIncluded only clears applied txs), so every retry re-hits it.
//
// NOTE: distinct from V6-H (resolveFinishMessage in the error-miss/timeout path) — this is the
// executor replay-vs-execute decision. Round 2 refuted the HasFinish asymmetry as "benign" by
// looking only at the StatusStarted branch (both arms storage-only, model already ran); this test
// drives the StatusPending branch where the model has NOT run, proving execution is suppressed.
func TestBugHunt_V6_GossipedJunkFinishSuppressesExecution(t *testing.T) {
	h, _, user, _ := newExecutorHostWithGossip(t) // host index 1 = executor for inference 1 (1%3==1)
	// Production wires a stored-response loader (manager.go:2245). This host never actually ran
	// inference 1, so the loader has no body for it.
	h.storedResponse = func(context.Context, string, uint64, uint64) ([]byte, error) { return nil, nil }

	// A Byzantine group peer gossips a JUNK Finish for inference 1 (no valid signature — neither
	// AddTx nor HasFinish verifies). It lands with the ProposedAt=0 peer sentinel.
	junk := &types.DevshardTx{Tx: &types.DevshardTx_FinishInference{
		FinishInference: &types.MsgFinishInference{InferenceId: 1, ServedHash: testutil.TestServedHash},
	}}
	h.HostMempool().AddTx(junk)

	// The user starts inference 1; host 1 is the executor and MUST run the model.
	diff1 := testutil.SignDiff(t, user, "escrow-1", 1, []*types.DevshardTx{testutil.StartTx(1)})
	resp, err := h.HandleRequest(context.Background(), HostRequest{
		Diffs: []types.Diff{diff1}, Nonce: 1, Payload: defaultPayload(),
	})
	require.NoError(t, err)

	require.NotNil(t, resp.ExecutionJob,
		"BUG V6-J: a peer-gossiped junk Finish for inference 1 (ProposedAt=0, unverified) makes "+
			"Mempool.HasFinish(1)==true (mempool.go:107 does not skip peer entries), so the StatusPending "+
			"executor path (host.go:1155) returns NO ExecutionJob — the real executor never runs the model "+
			"and the inference stalls to a miss. One junk tx from any group member suppresses a victim's "+
			"execution.")
}
