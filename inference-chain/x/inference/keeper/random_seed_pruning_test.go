package keeper_test

import (
	"testing"

	keepertest "github.com/productscience/inference/testutil/keeper"
	"github.com/productscience/inference/x/inference/types"
	"github.com/stretchr/testify/require"
)

// Consensus-halt audit (class 6 — unbounded state growth):
// RandomSeeds is keyed per (EpochIndex, participant) and written every epoch, but — unlike every
// sibling per-epoch PoC collection (store-commits, validations v1/v2, weight-distributions,
// snapshots) — it has NO pruner and is absent from keeper.Prune. It therefore grows without bound
// in consensus (IAVL) state: ~one entry per participant per epoch, forever -> long-term disk/
// commit-time bloat (a slow liveness/halt bottleneck).
//
// This asserts the CORRECT invariant (a seed from a pruned epoch must be gone, like its siblings),
// so it is RED on the current tree and GREEN once a RandomSeeds pruner is wired into Prune.
func TestRandomSeedsArePrunedWithSiblingPoCData(t *testing.T) {
	k, ctx := keepertest.InferenceKeeper(t)
	require.NoError(t, k.PruningState.Set(ctx, types.PruningState{}))
	setPruningConfig(ctx, k, PruningSettings{PocThreshold: 2}) // epochs <= current-2 are prunable

	require.NoError(t, k.SetEpoch(ctx, &types.Epoch{Index: 1, PocStartBlockHeight: 10}))
	require.NoError(t, k.SetEpoch(ctx, &types.Epoch{Index: 4, PocStartBlockHeight: 40}))

	addr := mkAddr(1)
	require.NoError(t, k.SetRandomSeed(ctx, types.RandomSeed{EpochIndex: 1, Participant: addr}))

	_, found := k.GetRandomSeed(ctx, 1, addr)
	require.True(t, found, "seed present before pruning")

	// Prune at current epoch 4 with PoC threshold 2 -> epoch 1 is well past the threshold and its
	// sibling PoC data (store-commits, validations, distributions for pocStart=10) is removed.
	require.NoError(t, k.Prune(ctx, 4))

	_, found = k.GetRandomSeed(ctx, 1, addr)
	require.Falsef(t, found,
		"RandomSeeds for epoch 1 must be pruned at current epoch 4 (threshold 2) like sibling PoC "+
			"collections; it is NOT (no pruner wired into keeper.Prune) -> unbounded state growth")
}
