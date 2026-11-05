package main

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// BUG V6-F (funds-lock): the escrow rotator's settle path deactivates the escrow in the store with
// SetDevshardActive(id,false) (escrow_rotator.go:784) BEFORE the failure-prone Finalize (:815) and
// on-chain broadcast (:845). SetDevshardActive leaves settlement_pending untouched
// (gateway_store_sqlite.go:716), so a failure after :784 leaves the row active=0, settlement_pending=0.
// Every recovery path keys on active||settlement_pending — reconcilePendingSettlements skips a row
// unless (!Active && SettlementPending) (gateway.go:4965) — so the escrow is orphaned: it holds real
// funds no automatic path can ever settle, and removeExpiredEpochStorage (gateway.go:4686) later
// prunes its storage because SettlementPending is false, making even manual settle impossible. The
// depletion path got this right with DeactivateDevshardIfActive(id,true) (:738, which marks pending).
// This drives the REAL SQLite store with the REAL store methods the code path uses (no mocks); the
// correct method is exercised on a control escrow to prove the settle path's choice is the defect.
func TestBugHunt_V6_RotatorSettleFailureOrphansEscrow(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteGatewayStore(filepath.Join(t.TempDir(), "gateway.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	// Two active rotated escrows holding funds.
	seed := []GatewayDevshardState{
		{RuntimeConfig: RuntimeConfig{ID: "12", Model: "m"}, Active: true, RotationRole: rotationRoleRegular},
		{RuntimeConfig: RuntimeConfig{ID: "13", Model: "m"}, Active: true, RotationRole: rotationRoleRegular},
	}
	require.NoError(t, store.Initialize(ctx, GatewaySettings{}, seed))

	// Escrow "12": the rotator settle path's pre-failure store op, exactly as escrow_rotator.go:784
	// (a transient host/chain hiccup then fails Finalize/broadcast after this point).
	require.NoError(t, store.SetDevshardActive(ctx, "12", false))

	// Escrow "13" (control): the depletion path's deactivation, which marks it settlement-pending.
	ok13, err := store.DeactivateDevshardIfActive(ctx, "13", true)
	require.NoError(t, err)
	require.True(t, ok13)

	st, loaded, err := store.LoadState(ctx)
	require.NoError(t, err)
	require.True(t, loaded)
	byID := gatewayDevshardsByID(st.Devshards)
	rec12, rec13 := byID["12"], byID["13"]

	// Control: the depletion path leaves "13" recoverable — reconcilePendingSettlements WOULD settle it
	// (its skip condition `Active || !SettlementPending` is false).
	require.False(t, rec13.Active)
	require.True(t, rec13.SettlementPending, "control: DeactivateDevshardIfActive keeps the escrow recoverable")
	require.False(t, rec13.Active || !rec13.SettlementPending, "control: reconcile would recover 13")

	// The invariant that MUST hold so some automatic path can recover escrow 12's funds:
	require.True(t, rec12.Active || rec12.SettlementPending,
		"BUG V6-F: a rotator settle that failed after escrow_rotator.go:784 left escrow 12 active=%t "+
			"settlement_pending=%t — invisible to every recovery path (reconcilePendingSettlements "+
			"requires !Active && SettlementPending, gateway.go:4965), so its funds are orphaned. The "+
			"depletion path avoids this with DeactivateDevshardIfActive(id,true).",
		rec12.Active, rec12.SettlementPending)
}
