package v0_2_14_claimrec

import (
	"context"

	upgradetypes "cosmossdk.io/x/upgrade/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	"github.com/productscience/inference/x/inference/keeper"
	"github.com/productscience/inference/x/inference/types"
)

// CreateUpgradeHandler builds the claim-recipient upgrade handler.
//
// The claim-recipient feature adds only two new collections to the EXISTING
// inference module KVStore:
//
//	ClaimRecipients        -> prefix 106
//	ClaimRecipientsByEpoch -> prefix 107
//
// These are key prefixes inside the already-mounted inference store key, not
// new store keys, so no StoreUpgrades / KVStoreKey mount is required. The
// collections lazy-init on first write (MsgSetClaimRecipients), so there is no
// state to backfill at upgrade time. The handler is therefore a near no-op: it
// only repeats the capability-version guard used by every handler in this app
// and runs migrations (which are no-ops for the inference module at the current
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
