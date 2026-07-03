package v0_2_14_deleg

import (
	"context"

	upgradetypes "cosmossdk.io/x/upgrade/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	"github.com/productscience/inference/x/inference/keeper"
	"github.com/productscience/inference/x/inference/types"
)

// CreateUpgradeHandler builds the delegation reward-only (PR #1378) upgrade
// handler on top of the v0.2.14-claimrec2 lineage.
//
// PR #1378 introduces a new singleton collection,
// DelegationRewardTransferSnapshot, at key prefix 108. The collection is
// created via collections.NewItem in NewKeeper against the EXISTING inference
// module store key, so no StoreUpgrades / KVStoreKey mount is required. The
// only state action needed is to seed an empty snapshot for the current
// effective epoch, so that the first settlement after the upgrade reads a
// well-defined (empty) snapshot rather than "not found". Seeding is idempotent.
func CreateUpgradeHandler(
	mm *module.Manager,
	configurator module.Configurator,
	k keeper.Keeper,
) upgradetypes.UpgradeHandler {
	return func(ctx context.Context, plan upgradetypes.Plan, fromVM module.VersionMap) (module.VersionMap, error) {
		k.LogInfo("starting upgrade", types.Upgrades, "version", UpgradeName)

		// The capability module ships without a version set even though it
		// exists, which makes RunMigrations attempt InitGenesis and panic.
		// Mirror the guard used by every other handler in this app.
		if _, ok := fromVM["capability"]; !ok {
			fromVM["capability"] = mm.Modules["capability"].(module.HasConsensusVersion).ConsensusVersion()
		}

		if err := seedDelegationRewardSnapshotForEffectiveEpoch(ctx, k); err != nil {
			return nil, err
		}

		toVM, err := mm.RunMigrations(ctx, configurator, fromVM)
		if err != nil {
			return toVM, err
		}

		k.LogInfo("successfully upgraded", types.Upgrades, "version", UpgradeName)
		return toVM, nil
	}
}

// seedDelegationRewardSnapshotForEffectiveEpoch seeds an empty
// DelegationRewardTransferSnapshot for the current effective epoch if none
// exists yet. It is idempotent: an already-present snapshot is left untouched
// (only a warning is logged if its epoch differs from the effective epoch).
// Ported from PR #1378's app/upgrades/v0_2_14/upgrades.go migration.
func seedDelegationRewardSnapshotForEffectiveEpoch(ctx context.Context, k keeper.Keeper) error {
	effectiveEpochIndex, found := k.GetEffectiveEpochIndex(ctx)
	if !found {
		k.LogError("seed delegation reward snapshot skipped: effective epoch not found", types.Upgrades)
		return nil
	}

	snapshot, snapshotFound := k.GetDelegationRewardTransferSnapshot(ctx)
	if snapshotFound {
		if snapshot.EpochIndex != effectiveEpochIndex {
			k.LogWarn("existing delegation reward snapshot epoch differs from effective epoch", types.Upgrades,
				"snapshot_epoch", snapshot.EpochIndex, "effective_epoch", effectiveEpochIndex)
		}
		return nil
	}

	if err := k.SetDelegationRewardTransferSnapshot(ctx, types.DelegationRewardTransferSnapshot{
		EpochIndex: effectiveEpochIndex,
	}); err != nil {
		return err
	}

	k.LogInfo("seeded delegation reward snapshot for effective epoch", types.Upgrades, "epoch", effectiveEpochIndex)
	return nil
}
