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

// CreateSetDecodeHandler cuts the active PoC scheme over to DECODE by setting
// poc_params.poc_scheme = POC_SCHEME_DECODE. The v0.2.17 migration already populated each
// model's schemes=[PREFILL,DECODE] (visible only via the keeper binary-proto store, not the
// lossy CLI JSON), so this flip selects the existing DECODE block for the next frozen PoC
// recipe. Done via a handler because MsgUpdateParams can't round-trip schemes[] on this chain
// (it would send schemes:null and wipe the migrated blocks). GetParams reads the full state.
func CreateSetDecodeHandler(
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
		if params.PocParams != nil {
			ns := 0
			if len(params.PocParams.Models) > 0 {
				ns = len(params.PocParams.Models[0].Schemes)
			}
			fmt.Printf("DECODE-CUTOVER: before poc_scheme=%v model0_schemes=%d\n", params.PocParams.PocScheme, ns)
			params.PocParams.PocScheme = inferencetypes.PocScheme_POC_SCHEME_DECODE
			if err := k.SetParams(ctx, params); err != nil {
				return nil, err
			}
			fmt.Printf("DECODE-CUTOVER: set poc_scheme=POC_SCHEME_DECODE (model0_schemes=%d)\n", ns)
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
	// Replay-existence handlers for the QA upgrades already applied on this tn3 chain
	// (g593a=alpha, seg=minPunishable, str=empty straddle). Bodies are NOT re-run on replay —
	// they only need to EXIST so x/upgrade does not error "handler is missing" when this v0.2.17
	// binary replays past those applied heights. Params they set are already in IAVL state.
	app.setTrackedUpgradeHandler("v0.2.17-g593a", CreateEmptyUpgradeHandler(app.ModuleManager, app.Configurator()))
	app.setTrackedUpgradeHandler("v0.2.17-seg", CreateEmptyUpgradeHandler(app.ModuleManager, app.Configurator()))
	app.setTrackedUpgradeHandler("v0.2.17-str", CreateEmptyUpgradeHandler(app.ModuleManager, app.Configurator()))
	// Decode cutover: flip active PoC scheme to DECODE (schemes[DECODE] already exists from the
	// v0.2.17 migration). Single-purpose: this binary adds v0.2.17-decode; cosmovisor swaps to it
	// only at the decode upgrade height.
	app.setTrackedUpgradeHandler("v0.2.17-decode", CreateSetDecodeHandler(app.ModuleManager, app.Configurator(), app.InferenceKeeper))
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
