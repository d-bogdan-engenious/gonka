//go:build !upgraded

package app

import (
	"context"
	"fmt"

	upgradetypes "cosmossdk.io/x/upgrade/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	districutiontypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/productscience/inference/app/upgrades/v0_2_10"
	"github.com/productscience/inference/app/upgrades/v0_2_11"
	"github.com/productscience/inference/app/upgrades/v0_2_12"
	"github.com/productscience/inference/app/upgrades/v0_2_13"
	"github.com/productscience/inference/app/upgrades/v0_2_14"
	"github.com/productscience/inference/app/upgrades/v0_2_15"
	"github.com/productscience/inference/app/upgrades/v0_2_16"
	"github.com/productscience/inference/app/upgrades/v0_2_17"
	v0_2_2 "github.com/productscience/inference/app/upgrades/v0_2_2"
	v0_2_3 "github.com/productscience/inference/app/upgrades/v0_2_3"
	"github.com/productscience/inference/app/upgrades/v0_2_4"
	"github.com/productscience/inference/app/upgrades/v0_2_5"
	"github.com/productscience/inference/app/upgrades/v0_2_6"
	"github.com/productscience/inference/app/upgrades/v0_2_7"
	"github.com/productscience/inference/app/upgrades/v0_2_8"
	"github.com/productscience/inference/app/upgrades/v0_2_9"
	inferencekeeper "github.com/productscience/inference/x/inference/keeper"
	inferencetypes "github.com/productscience/inference/x/inference/types"
)

func CreateEmptyUpgradeHandler(
	mm *module.Manager,
	configurator module.Configurator) upgradetypes.UpgradeHandler {
	return func(ctx context.Context, plan upgradetypes.Plan, vm module.VersionMap) (module.VersionMap, error) {

		for moduleName, version := range vm {
			fmt.Printf("Module: %s, Version: %d\n", moduleName, version)
		}
		fmt.Printf("OrderMigrations: %v\n", mm.OrderMigrations)

		// For some reason, the capability module doesn't have a version set, but it DOES exist, causing
		// the `InitGenesis` to panic.
		if _, ok := vm["capability"]; !ok {
			vm["capability"] = mm.Modules["capability"].(module.HasConsensusVersion).ConsensusVersion()
		}
		return mm.RunMigrations(ctx, configurator, vm)
	}
}

// CreateSetAlphaUpgradeHandler sets confirmation_poc_params.alpha_threshold=0.01 in Go
// (keeper store round-trip is proto-lossless, so decode scheme blocks are preserved) and
// runs migrations. Needed because MsgUpdateParams cannot carry the per-model schemes field
// on this chain, so alpha (required to open a PoC challenge) must be set via a handler.
func CreateSetAlphaUpgradeHandler(
	mm *module.Manager,
	configurator module.Configurator,
	k inferencekeeper.Keeper,
) upgradetypes.UpgradeHandler {
	return func(ctx context.Context, plan upgradetypes.Plan, vm module.VersionMap) (module.VersionMap, error) {
		if _, ok := vm["capability"]; !ok {
			vm["capability"] = mm.Modules["capability"].(module.HasConsensusVersion).ConsensusVersion()
		}
		params, err := k.GetParams(ctx)
		if err != nil {
			return nil, err
		}
		if params.ConfirmationPocParams == nil {
			params.ConfirmationPocParams = &inferencetypes.ConfirmationPoCParams{}
		}
		params.ConfirmationPocParams.AlphaThreshold = inferencetypes.DecimalFromFloat(0.01)
		if err := k.SetParams(ctx, params); err != nil {
			return nil, err
		}
		return mm.RunMigrations(ctx, configurator, vm)
	}
}

func (app *App) setupUpgradeHandlers() {
	app.Logger().Info("Setting up upgrade handlers")
	upgradeInfo, err := app.UpgradeKeeper.ReadUpgradeInfoFromDisk()
	if err != nil {
		app.Logger().Error("Failed to read upgrade info from disk", "error", err)
		return
	}
	app.Logger().Info("Applying upgrade", "upgradeInfo", upgradeInfo)

	app.setTrackedUpgradeHandler(v0_2_2.UpgradeName, v0_2_2.CreateUpgradeHandler(app.ModuleManager, app.Configurator(), app.InferenceKeeper))
	app.setTrackedUpgradeHandler(v0_2_3.UpgradeName, v0_2_3.CreateUpgradeHandler(app.ModuleManager, app.Configurator(), app.InferenceKeeper))
	app.setTrackedUpgradeHandler(v0_2_4.UpgradeName, v0_2_4.CreateUpgradeHandler(app.ModuleManager, app.Configurator(), app.InferenceKeeper))
	app.setTrackedUpgradeHandler(v0_2_5.UpgradeName, v0_2_5.CreateUpgradeHandler(app.ModuleManager, app.Configurator(), app.InferenceKeeper, app.BlsKeeper))
	app.setTrackedUpgradeHandler(v0_2_6.UpgradeName, v0_2_6.CreateUpgradeHandler(app.ModuleManager, app.Configurator(), app.InferenceKeeper, app.DistrKeeper))
	app.setTrackedUpgradeHandler(v0_2_7.UpgradeName, v0_2_7.CreateUpgradeHandler(app.ModuleManager, app.Configurator(), app.InferenceKeeper, app.DistrKeeper))
	app.setTrackedUpgradeHandler(v0_2_8.UpgradeName, v0_2_8.CreateUpgradeHandler(app.ModuleManager, app.Configurator(), app.InferenceKeeper, app.BlsKeeper, app.DistrKeeper, app.AuthzKeeper))
	app.setTrackedUpgradeHandler(v0_2_9.UpgradeName, v0_2_9.CreateUpgradeHandler(app.ModuleManager, app.Configurator(), app.InferenceKeeper))
	app.setTrackedUpgradeHandler(v0_2_10.UpgradeName, v0_2_10.CreateUpgradeHandler(app.ModuleManager, app.Configurator(), app.InferenceKeeper, app.DistrKeeper))
	app.setTrackedUpgradeHandler(v0_2_11.UpgradeName, v0_2_11.CreateUpgradeHandler(app.ModuleManager, app.Configurator(), app.InferenceKeeper, app.DistrKeeper, app.BlsKeeper))
	app.setTrackedUpgradeHandler(v0_2_12.UpgradeName, v0_2_12.CreateUpgradeHandler(app.ModuleManager, app.Configurator(), app.InferenceKeeper, app.DistrKeeper, app.BlsKeeper, app.AuthzKeeper, app.FeeGrantKeeper))
	app.setTrackedUpgradeHandler(v0_2_13.UpgradeName, v0_2_13.CreateUpgradeHandler(app.ModuleManager, app.Configurator(), app.InferenceKeeper, app.AuthzKeeper, app.GovKeeper))
	app.setTrackedUpgradeHandler(v0_2_14.UpgradeName, v0_2_14.CreateUpgradeHandler(app.ModuleManager, app.Configurator(), app.InferenceKeeper, app.GenesistransferKeeper, app.MintKeeper))
	app.setTrackedUpgradeHandler(v0_2_15.UpgradeName, v0_2_15.CreateUpgradeHandler(app.ModuleManager, app.Configurator(), app.InferenceKeeper, app.AuthzKeeper))
	app.setTrackedUpgradeHandler(v0_2_16.UpgradeName, v0_2_16.CreateUpgradeHandler(app.ModuleManager, app.Configurator(), app.InferenceKeeper, app.AuthzKeeper))
	app.setTrackedUpgradeHandler(v0_2_17.UpgradeName, v0_2_17.CreateUpgradeHandler(app.ModuleManager, app.Configurator(), app.InferenceKeeper))
	// QA GON-665/#2206: v0.2.17 already applied on tn3 (name consumed); this fresh name lets us
	// gov-upgrade the binary to the #2206-inclusive build. #2206 is a pure validation-gate fix with
	// no state migration, so an empty passthrough handler (capability fix + idempotent RunMigrations)
	// is the correct, safe handler. The name MUST match the gov proposal or the chain halts at height.
	app.setTrackedUpgradeHandler("v0.2.17-2206", CreateEmptyUpgradeHandler(app.ModuleManager, app.Configurator()))
	// QA GON-593 live: set CPoC alpha (handler, since MsgUpdateParams can't carry schemes),
	// then a second empty "straddle" upgrade run while a long-running PoC challenge is OPEN.
	app.setTrackedUpgradeHandler("v0.2.17-alpha", CreateSetAlphaUpgradeHandler(app.ModuleManager, app.Configurator(), app.InferenceKeeper))
	app.setTrackedUpgradeHandler("v0.2.17-straddle", CreateEmptyUpgradeHandler(app.ModuleManager, app.Configurator()))
}

func (app *App) registerMigrations() {
	app.Configurator().RegisterMigration(inferencetypes.ModuleName, 4, func(ctx sdk.Context) error {
		return nil
	})

	app.Configurator().RegisterMigration(inferencetypes.ModuleName, 5, func(ctx sdk.Context) error {
		return nil
	})

	app.Configurator().RegisterMigration(inferencetypes.ModuleName, 6, func(ctx sdk.Context) error {
		return nil
	})

	// v0.2.5 upgrade migrations
	app.Configurator().RegisterMigration(inferencetypes.ModuleName, 7, func(ctx sdk.Context) error {
		if err := app.InferenceKeeper.MigrateLegacyBridgeState(ctx); err != nil {
			return err
		}
		return app.InferenceKeeper.MigrateConfirmationWeights(ctx)
	})

	app.Configurator().RegisterMigration(inferencetypes.ModuleName, 8, func(ctx sdk.Context) error {
		return nil
	})

	app.Configurator().RegisterMigration(inferencetypes.ModuleName, 9, func(ctx sdk.Context) error {
		return nil
	})

	app.Configurator().RegisterMigration(inferencetypes.ModuleName, 10, func(ctx sdk.Context) error {
		return nil
	})

	app.Configurator().RegisterMigration(inferencetypes.ModuleName, 11, func(ctx sdk.Context) error {
		return nil
	})

	app.Configurator().RegisterMigration(districutiontypes.ModuleName, 3, func(ctx sdk.Context) error {
		return nil
	})

	app.Configurator().RegisterMigration(slashingtypes.ModuleName, 4, func(ctx sdk.Context) error {
		return nil
	})

	app.Configurator().RegisterMigration(stakingtypes.ModuleName, 5, func(ctx sdk.Context) error {
		return nil
	})

	app.Configurator().RegisterMigration(inferencetypes.ModuleName, 11, func(ctx sdk.Context) error {
		return nil
	})

	app.Configurator().RegisterMigration(inferencetypes.ModuleName, 12, func(ctx sdk.Context) error { return nil })

	app.Configurator().RegisterMigration(inferencetypes.ModuleName, 13, func(ctx sdk.Context) error { return nil })

	app.Configurator().RegisterMigration(inferencetypes.ModuleName, 14, func(ctx sdk.Context) error {
		return app.InferenceKeeper.MigrateFeeParamsToTree(ctx)
	})
}
