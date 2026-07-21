// Package v0_2_14b is a no-op re-upgrade handler used to re-run the coordinated
// dual-binary (inferenced + dapi) swap on a chain already at 0.2.14. It performs
// no new state migrations.
package v0_2_14b

import (
	"context"

	upgradetypes "cosmossdk.io/x/upgrade/types"
	"github.com/cosmos/cosmos-sdk/types/module"
)

func CreateUpgradeHandler(mm *module.Manager, configurator module.Configurator) upgradetypes.UpgradeHandler {
	return func(ctx context.Context, plan upgradetypes.Plan, fromVM module.VersionMap) (module.VersionMap, error) {
		return mm.RunMigrations(ctx, configurator, fromVM)
	}
}
