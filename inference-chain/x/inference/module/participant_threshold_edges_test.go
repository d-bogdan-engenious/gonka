package inference

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Edge-branch coverage for calculateParticipantWeightThreshold75Percent (was 69.2%;
// dev test covers only the multi-weight happy path). This is the active-set 75%
// participant cutoff — the branches below are the "returns 0 / can't reach target"
// paths that decide whether a participant qualifies at all.
func TestCalculateParticipantWeightThreshold75Percent_Edges(t *testing.T) {
	tests := []struct {
		name      string
		weights   []int64
		liveTotal int64
		want      int64
	}{
		{"empty weights -> 0", nil, 100, 0},
		{"liveRootTotal <= 0 -> 0", []int64{80}, 0, 0},
		{"single below 75% target -> 0", []int64{50}, 100, 0},            // target=75, 50<75
		{"single at/above target -> w-1", []int64{80}, 100, 79},          // target=75, 80>=75 -> 79
		{"multi below aggregate target -> 0", []int64{30, 30}, 100, 0},   // target=75, sum 60<75
		{"multi reaches target -> cutoff", []int64{40, 40}, 100, 40},     // target=75, sum 80>=75, uniform cutoff
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, calculateParticipantWeightThreshold75Percent(tc.weights, tc.liveTotal))
		})
	}
}
