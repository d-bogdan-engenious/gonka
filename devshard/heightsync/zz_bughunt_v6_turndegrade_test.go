package heightsync_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"devshard/heightsync"
	"devshard/types"
)

// BUG V6-E (heightsync liveness / Byzantine-triggerable): TurnTracker.windowClosed (turn.go:331)
// treats the turn's ack window as closed if the MAX height over ALL recorded acks exceeds the
// deadline — so a single over-stated ack (a host reporting a height past the window while its real
// tip is still inside it) closes the window for everyone. Because recompute early-returns once a
// turn leaves TurnOpen, folding that one ack FIRST degrades the turn, and the honest quorum that
// arrives afterwards is force-marked Late and never counts. The exact same three acks folded first
// would COMPLETE the turn — the outcome is wrongly dependent on diff/nonce ordering, and an L0/L4
// over-statement is only marked, never rejected, so one Byzantine host can prematurely degrade
// every open under-quorum turn. Driven through the real TurnTracker.Observe/recompute path.
func TestBugHunt_V6_OverstatedAckPrematurelyDegradesTurn(t *testing.T) {
	cfg := heightsync.DefaultHeartbeatConfig()
	D := cfg.AckDeadlineBlocks
	require.GreaterOrEqual(t, D, uint64(12), "test assumes a non-trivial ack window; config changed?")
	const hReq = uint64(500)
	deadline := hReq + D
	past := deadline + 1 // an over-stated ack height, past the window

	// Control: the SAME acks in the opposite order (honest quorum first, over-stated ack last)
	// complete the turn — proving the outcome is wrongly dependent on fold order.
	ctrl := heightsync.NewTurnTracker(4, 3, cfg)
	ctrl.Observe(10, []*types.DevshardTx{heartbeatTx(hReq, 4)}, hReq)
	ctrl.Observe(14, []*types.DevshardTx{
		ackTx(10, 0, 510, types.SyncState_SYNCED),
		ackTx(10, 1, 510, types.SyncState_SYNCED),
		ackTx(10, 2, 510, types.SyncState_SYNCED),
	}, 511)
	ctrl.Observe(20, []*types.DevshardTx{ackTx(10, 3, past, types.SyncState_SYNCED)}, hReq+1)
	require.Equal(t, heightsync.TurnComplete, ctrl.Record(ctrl.LatestTurnStart()).State,
		"control: honest quorum folded first completes the turn")

	tr := heightsync.NewTurnTracker(4, 3, cfg) // 4 slots, quorum 3
	tr.Observe(10, []*types.DevshardTx{heartbeatTx(hReq, 4)}, hReq)

	// One over-stated ack from slot 3 (its real tip is still in-window; it just reports a height
	// past the deadline). Folded FIRST.
	tr.Observe(14, []*types.DevshardTx{ackTx(10, 3, past, types.SyncState_SYNCED)}, hReq+1)

	// A full honest quorum (slots 0,1,2) then acks in-window (height 510 <= deadline).
	tr.Observe(20, []*types.DevshardTx{
		ackTx(10, 0, 510, types.SyncState_SYNCED),
		ackTx(10, 1, 510, types.SyncState_SYNCED),
		ackTx(10, 2, 510, types.SyncState_SYNCED),
	}, 511)

	rec := tr.Record(tr.LatestTurnStart())
	require.Equal(t, heightsync.TurnComplete, rec.State,
		"BUG V6-E: a quorum of 3 in-window acks (height 510 <= deadline %d) should COMPLETE the turn, "+
			"but one over-stated ack folded first closed the window early (windowClosed uses the MAX ack "+
			"height, turn.go:331) and degraded it — the honest quorum arriving after can never un-degrade. "+
			"The identical acks in the opposite order complete the turn.", deadline)
}
