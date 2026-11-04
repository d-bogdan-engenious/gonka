package host

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"devshard/heightsync"
	"devshard/internal/testutil"
	"devshard/signing"
	"devshard/state"
	"devshard/stub"
	"devshard/types"
)

// BUG V6-C (two real Host instances, real repair exchange): a validator A that owns several
// slots answers a repair probe for its NON-primary slot with a courtesy ack attributed to its
// PrimarySlot (host/repair.go builds the ack with slot = h.PrimarySlot() instead of
// heightsync.SlotForNonce(req.RefNonce)). The requester B folds/queues the ack by its SlotId,
// so the probed slot's missing ack is never cleared and the non-primary slot can never repair.
//
// This is not a single-function unit and not a mock: it drives the ACTUAL request -> response
// -> ingest path between two real Host instances, each backed by a real StateMachine and a real
// chain oracle, through the real BuildRepairHeightResponse (responder half) and the real
// ingestRepairHeight (requester half, including the real VerifyAckAllowed signature check and
// the real mempool queue). The observable is the ack B actually queues for inclusion.
func TestBugHunt_V6_RepairExchange_NonPrimarySlotNeverRepairs(t *testing.T) {
	ctx := context.Background()
	aSig := testutil.MustGenerateKey(t) // validator A: owns slots 0 AND 1 (responder, multi-slot)
	bSig := testutil.MustGenerateKey(t) // validator B: owns slot 2 (requester)
	user := testutil.MustGenerateKey(t)
	group := []types.SlotAssignment{
		{SlotID: 0, ValidatorAddress: aSig.Address()},
		{SlotID: 1, ValidatorAddress: aSig.Address()},
		{SlotID: 2, ValidatorAddress: bSig.Address()},
	}
	config := testutil.DefaultConfig(len(group))
	verifier := signing.NewSecp256k1Verifier()

	newHost := func(signer *signing.Secp256k1Signer) *Host {
		sm, err := state.NewStateMachine("escrow-1", config, group, 100000, user.Address(), verifier,
			testutil.MustMemoryStore(t, "escrow-1", user.Address(), config, group, 100000))
		require.NoError(t, err)
		or := &fakeOracle{}
		or.setHeight(500)
		or.setHash([]byte{0xaa})
		h, err := NewHost(sm, signer, stub.NewInferenceEngine(), "escrow-1", group, nil, WithGrace(100), WithChainOracle(or))
		require.NoError(t, err)
		return h
	}
	responderA := newHost(aSig) // PrimarySlot = 0
	requesterB := newHost(bSig)
	require.Equal(t, uint32(0), responderA.PrimarySlot(), "A's primary is slot 0; slot 1 is non-primary")

	// Open turn 1 on both hosts via a quiet heartbeat span (slots=3) so the responder has a turn
	// record (BuildRepairHeightResponse rejects unknown turns).
	const slots = uint64(3)
	hb := []types.Diff{
		heartbeatDiff(t, user, 1, 1, 500, slots),
		heartbeatDiff(t, user, 2, 1, 500, slots),
		heartbeatDiff(t, user, 3, 1, 500, slots),
	}
	_, err := responderA.HandleRequest(ctx, HostRequest{Diffs: hb})
	require.NoError(t, err)
	_, err = requesterB.HandleRequest(ctx, HostRequest{Diffs: hb})
	require.NoError(t, err)
	require.NotNil(t, responderA.HeightSyncTurnRecord(1), "A opened turn 1")

	// B probes A for A's NON-primary slot 1 (RefNonce addresses slot 1).
	const probedSlot = uint32(1)
	req := &heightsync.RepairRequest{
		TurnStart:         1,
		RefNonce:          heightsync.HeartbeatNonceForSlot(1, probedSlot, uint32(len(group))),
		RequesterSlot:     2,
		ObservedHeight:    500,
		ObservedBlockHash: []byte{0xaa},
	}
	require.NoError(t, heightsync.SignRepairRequest(bSig, req))

	// B already self-acked its own slot (2) when it opened turn 1; record that so we can isolate
	// the ack the repair exchange newly queues.
	before := map[uint32]bool{}
	for _, tx := range requesterB.MempoolTxs() {
		if a := tx.GetHeightAck(); a != nil {
			before[a.SlotId] = true
		}
	}

	resp, err := responderA.BuildRepairHeightResponse(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp.Ack, "A returns a courtesy ack for the probe")

	// B ingests the REAL repair response: real VerifyAckAllowed, then queued to B's mempool.
	requesterB.ingestRepairHeight(probedSlot, resp)

	// Isolate the ack the repair exchange newly added to B's mempool.
	queued := -1
	for _, tx := range requesterB.MempoolTxs() {
		if a := tx.GetHeightAck(); a != nil && !before[a.SlotId] {
			queued = int(a.SlotId)
		}
	}
	require.GreaterOrEqual(t, queued, 0, "B queued a new ack from the repair exchange")

	// Consequence end-to-end: B probed A for slot 1, but the ack the exchange queued is attributed
	// to A's PrimarySlot (0), so slot 1's missing ack is never cleared and the non-primary slot can
	// never repair.
	require.Equal(t, int(probedSlot), queued,
		"BUG V6-C: B probed A for slot 1, but the repair ack A built (and B queued for inclusion) is "+
			"attributed to A's PrimarySlot (0), not the probed slot 1 — so slot 1 never repairs "+
			"(host/repair.go uses h.PrimarySlot() instead of heightsync.SlotForNonce(req.RefNonce))")
}
