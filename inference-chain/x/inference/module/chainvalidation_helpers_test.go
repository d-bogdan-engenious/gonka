package inference

import (
	"testing"

	"github.com/productscience/inference/x/inference/types"
	"github.com/stretchr/testify/require"
)

// Coverage for previously-0% pure helpers in chainvalidation.go.

func TestCalculateTotalWeight(t *testing.T) {
	require.Equal(t, uint64(0), calculateTotalWeight(nil), "nil map -> 0")
	require.Equal(t, uint64(0), calculateTotalWeight(map[string]int64{}), "empty map -> 0")
	require.Equal(t, uint64(8), calculateTotalWeight(map[string]int64{"a": 5, "b": 3}), "sums positives")
	require.Equal(t, uint64(5), calculateTotalWeight(map[string]int64{"a": 5, "b": -2}),
		"negative weight is skipped, not subtracted")
}

// mergeMLNodeArrays is DEAD CODE (no callers) and also broken: it calls
// mergeByModel(nil, preserved, nil, poc) with nil MODEL-NAME lists, but mergeByModel iterates
// those (nil) lists to drive the merge, so no nodes are ever processed -> it ALWAYS returns nil,
// regardless of input. Characterized here (NOT a live bug: nothing invokes it; delete candidate).
func TestMergeMLNodeArrays_DeadBrokenLegacyAlwaysNil(t *testing.T) {
	require.Nil(t, mergeMLNodeArrays(nil, nil))
	require.Nil(t, mergeMLNodeArrays(
		[]*types.ModelMLNodes{{MlNodes: []*types.MLNodeInfo{{NodeId: "preserved-1"}}}},
		[]*types.ModelMLNodes{{MlNodes: []*types.MLNodeInfo{{NodeId: "poc-1"}}}},
	), "always nil even with real nodes -> dead + broken")
}
