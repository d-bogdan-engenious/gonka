package inference

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/productscience/inference/x/inference/types"
	"github.com/stretchr/testify/require"
)

// Covers the computeNewWeights branch where a store-commit exists for an address
// that is NOT a registered participant: the key passes the (empty) allowlist, then
// GetParticipant misses and the commit is skipped. Security-relevant — a commit
// from an unregistered address must never earn weight.
func TestComputeNewWeights_SkipsUnregisteredParticipant(t *testing.T) {
	f := newFormationRecoveryFixture(t, noopCollateralKeeper{}, 0, 2)
	f.addFreshPoC(t, 100) // legitimate commit/validation for the registered newcomer

	bogus := sdk.AccAddress([]byte("bogus-unregistered-1")).String()
	require.NoError(t, f.keeper.SetPoCV2StoreCommit(f.ctx, types.PoCV2StoreCommit{
		ParticipantAddress:       bogus,
		PocStageStartBlockHeight: f.upcomingEpoch.PocStartBlockHeight,
		Count:                    50,
		ModelId:                  f.modelID,
	}))
	if _, ok := f.keeper.GetParticipant(f.ctx, bogus); ok {
		t.Fatalf("precondition: %q must not be a registered participant", bogus)
	}

	cw := f.module.computeNewWeights(f.ctx, f.upcomingEpoch)
	for _, p := range cw.participants {
		require.NotEqualf(t, bogus, p.Index,
			"unregistered address %q with a store commit must be excluded from computed weights", bogus)
	}
}
