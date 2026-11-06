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
// (keeper store round-trip is proto-lossless, so any per-model scheme blocks are preserved)
// and runs migrations. MsgUpdateParams cannot carry the per-model schemes field on this
// chain, so alpha (required to OPEN a PoC challenge, else code 1204) must be set via a
// handler. GON-593 live: enables opening a long-running PoC challenge before a later upgrade.
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

// CreateSetMinPunishableSegHandler lowers poc_challenge_params.min_punishable_segment_blocks
// to 50 so a PoC challenge can be opened on a short-epoch testnet (epoch_length=280). The
// default is 300, which requires (nextPoCStart - safetyWindow - 300) >= inference-phase start
// — impossible when epoch_length < ~425, so create-poc-challenge rejects with code 1204
// "remaining segment shorter than 300 blocks". Keeps alpha set (idempotent) and preserves
// MaxActiveChallenges (>0, required by Validate).
func CreateSetMinPunishableSegHandler(
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
		if params.PocChallengeParams == nil {
			params.PocChallengeParams = inferencetypes.DefaultPoCChallengeParams()
		}
		params.PocChallengeParams.MinPunishableSegmentBlocks = 50
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
	// QA GON-593 live (enabler upgrade): set CPoC alpha_threshold so a long-running PoC
	// challenge can be OPENed. SINGLE-PURPOSE binary — this build registers ONLY g593a and
	// deliberately does NOT register g593b, so that while the chain runs this binary and the
	// later g593b plan is scheduled, the x/upgrade "BINARY UPDATED BEFORE TRIGGER" guard does
	// not fire (that guard is what halted a prior combined-handler binary).
	app.setTrackedUpgradeHandler("v0.2.17-g593a", CreateSetAlphaUpgradeHandler(app.ModuleManager, app.Configurator(), app.InferenceKeeper))
	// GON-593 (enabler #2): lower min_punishable_segment_blocks so a challenge fits epoch 280.
	// SINGLE-PURPOSE: this binary registers g593a (replay, already applied) + seg ONLY, NOT str,
	// so scheduling the later str plan while running this binary does not trip the guard.
	app.setTrackedUpgradeHandler("v0.2.17-seg", CreateSetMinPunishableSegHandler(app.ModuleManager, app.Configurator(), app.InferenceKeeper))
	// GON-593 (straddle): the empty upgrade that runs WHILE a long-running PoC challenge is OPEN.
	// Carries g593a + seg handlers so replay past those applied heights does not fail; cosmovisor
	// swaps to THIS binary only at the str trigger height.
	app.setTrackedUpgradeHandler("v0.2.17-str", CreateEmptyUpgradeHandler(app.ModuleManager, app.Configurator()))
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
