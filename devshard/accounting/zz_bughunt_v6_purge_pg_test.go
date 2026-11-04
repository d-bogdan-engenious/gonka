package accounting

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// BUG V6-B (real Postgres, testcontainers): PurgeEpoch deletes the in-memory escrow and
// Flushes, but never records the removal in pendingDeletes, so takePersistSnapshot emits an
// empty deletedIDs and the Postgres backend never DELETEs the row. On reload (a restart, or
// any other live HA instance) the purged epoch's ledger — with its charged counters —
// resurrects. This runs against a real throwaway Postgres via testcontainers (the actual
// postgres backend), NOT sqlite and NOT an in-memory mock, so the PG-only defect is exercised
// end to end. SQLite masks it by rewriting the whole table; Postgres honours deletedIDs.
func TestBugHunt_V6_PurgeEpochResurrectsFromPostgresOnReload(t *testing.T) {
	setupAccountingPostgres(t) // real Postgres container + DEVSHARD_STORAGE_MODE=postgres
	ctx := context.Background()

	a := openWriterTracker(t, "writer-purge")
	registerEscrow(t, a, "e-purge", 9, "m")
	require.NoError(t, a.RecordDiff("e-purge", 1, false))
	require.NoError(t, a.Flush(ctx))
	require.NotEmpty(t, a.Query(QueryFilter{EpochIndex: 9}),
		"precondition: the escrow charged in epoch 9 is in the ledger before purge")

	removed, err := a.PurgeEpoch(ctx, 9)
	require.NoError(t, err)
	require.Equal(t, 1, removed)
	require.NoError(t, a.Close())

	// Reload the ledger from the SAME Postgres (equivalent to a restart or a second HA instance).
	b := openWriterTracker(t, "writer-purge")
	defer b.Close()
	require.Empty(t, b.Query(QueryFilter{EpochIndex: 9}),
		"BUG V6-B: PurgeEpoch did not record the deletion, so the purged epoch's ledger resurrects "+
			"from Postgres on reload — the escrow and its charged counters come back")
}
