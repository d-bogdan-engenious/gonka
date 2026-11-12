package inference

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Covers getCurrentValidatorWeights (was 0%): reads the current epoch group's
// members and parses their on-chain weight strings into a map. Reuses the
// formation fixture, whose effective-epoch root group carries ValidationWeights
// (veteran=100, delegator=49) resolved to group members by the stub group keeper.
func TestGetCurrentValidatorWeights_ParsesGroupMemberWeights(t *testing.T) {
	f := newFormationRecoveryFixture(t, noopCollateralKeeper{}, 0, 2)

	weights, err := f.module.getCurrentValidatorWeights(f.ctx)
	require.NoError(t, err)
	require.NotEmpty(t, weights, "current group members should resolve to weights")

	// The root epoch group for the effective epoch was seeded with these.
	require.Equal(t, int64(100), weights[f.veteran], "veteran weight parsed from member")
	require.Equal(t, int64(49), weights[f.delegator], "delegator weight parsed from member")
}
