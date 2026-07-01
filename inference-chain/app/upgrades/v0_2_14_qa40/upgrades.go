package v0_2_14_qa40

import (
	"context"

	upgradetypes "cosmossdk.io/x/upgrade/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	"github.com/productscience/inference/x/inference/keeper"
	"github.com/productscience/inference/x/inference/types"
)

// CreateUpgradeHandler builds the claim-recipient (devshard routing fix)
// upgrade handler.
//
// PR #1377 makes late-settle and unsettled->prune devshard payouts respect the
// per-(participant, epoch) claim-recipient override, and retains the override
// entry until it is pruned once the epoch is safely stale. These are CODE-ONLY
// changes: they reuse the EXISTING claim-recipient collections added by the
// v0.2.14-claimrec build:
//
//	ClaimRecipients        -> prefix 106
//	ClaimRecipientsByEpoch -> prefix 107
//
// No new key prefixes and no new store keys are introduced, so no
// StoreUpgrades / KVStoreKey mount is required and there is no state to
// backfill. The handler is therefore a near no-op: it only repeats the
// capability-version guard used by every handler in this app and runs
// migrations (which are no-ops for the inference module at the current
// consensus version).
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

		toVM, err := mm.RunMigrations(ctx, configurator, fromVM)
		if err != nil {
			return toVM, err
		}

		k.LogInfo("successfully upgraded", types.Upgrades, "version", UpgradeName)
		return toVM, nil
	}
}
