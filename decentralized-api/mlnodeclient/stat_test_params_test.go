package mlnodeclient

import (
	"encoding/json"
	"testing"

	"github.com/productscience/inference/x/inference/types"
	"github.com/stretchr/testify/require"
)

// QA / decode fraud-threshold plumbing: StatTestParamsFromChain maps the chain's per-model
// stat-test thresholds (dist_threshold, p_mismatch, p_value/alpha) onto the wire struct the
// ML node reads. The ML node's fraud sensitivity is entirely governed by these three numbers,
// so a silent drop here means fraud is scored at the wrong threshold.
//
// Reachability: nothing on the chain validates these thresholds (no Validate on
// PoCStatTestParams), and DecimalFromFloat(0) is a NON-nil zero, so a governance/model config
// of 0 is a legitimate, reachable value.

func TestStatTestParamsFromChain_NilFallsBackToDefaults(t *testing.T) {
	got := StatTestParamsFromChain(nil)
	require.Equal(t, DefaultDistThreshold, got.DistThreshold)
	require.Equal(t, DefaultPMismatch, got.PMismatch)
	require.Equal(t, DefaultPValueThreshold, got.PValueThreshold)

	// Per-field nil: only alpha set -> dist & p_mismatch default, alpha passes through.
	only := StatTestParamsFromChain(&types.PoCStatTestParams{PValueThreshold: types.DecimalFromFloat(0.02)})
	require.Equal(t, DefaultDistThreshold, only.DistThreshold)
	require.Equal(t, DefaultPMismatch, only.PMismatch)
	require.InDelta(t, 0.02, only.PValueThreshold, 1e-9)
}

func TestStatTestParamsFromChain_NonZeroReachMLNodeUnderCorrectWireNames(t *testing.T) {
	body, err := json.Marshal(StatTestParamsFromChain(&types.PoCStatTestParams{
		DistThreshold:   types.DecimalFromFloat(0.3),
		PMismatch:       types.DecimalFromFloat(0.2),
		PValueThreshold: types.DecimalFromFloat(0.02),
	}))
	require.NoError(t, err)
	var wire map[string]float64
	require.NoError(t, json.Unmarshal(body, &wire))
	require.InDelta(t, 0.3, wire["dist_threshold"], 1e-9)
	require.InDelta(t, 0.2, wire["p_mismatch"], 1e-9)
	require.InDelta(t, 0.02, wire["fraud_threshold"], 1e-9) // alpha goes out under this name
	require.NotContains(t, wire, "p_value_threshold")
}

// Coverage/characterization (NOT a new bug — known GON-489-area plumbing): the code falls
// back to defaults only on nil, so a chain-specified 0 is kept as 0.0 (the doc comment's
// "or zero" is not implemented). Documents the non-nil-zero path.
func TestStatTestParamsFromChain_ZeroKeptNotDefaulted(t *testing.T) {
	got := StatTestParamsFromChain(&types.PoCStatTestParams{
		DistThreshold:   types.DecimalFromFloat(0),
		PMismatch:       types.DecimalFromFloat(0),
		PValueThreshold: types.DecimalFromFloat(0),
	})
	require.Equal(t, 0.0, got.DistThreshold)
	require.Equal(t, 0.0, got.PMismatch)
	require.Equal(t, 0.0, got.PValueThreshold)
}

// Coverage/characterization: omitempty drops a 0 from the wire (known GON-489-area behavior).
func TestStatTestParamsV2_ZeroDroppedByOmitempty(t *testing.T) {
	body, err := json.Marshal(&StatTestParamsV2{DistThreshold: 0, PMismatch: 0, PValueThreshold: 0})
	require.NoError(t, err)
	var wire map[string]float64
	require.NoError(t, json.Unmarshal(body, &wire))
	require.Empty(t, wire)
}
