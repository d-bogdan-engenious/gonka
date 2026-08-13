package inference

// Adversarial bypass hunt for the GON-466 provenance-preserving seating fix.
// Each test tries to get weight onto a RICH model (sorts first in governance
// order = the pre-fix capture position) that the node never proved. Any failing
// assertion is a real bypass.

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/productscience/inference/x/inference/types"
)

const (
	bpRich  = "aaa-rich/Model"  // sorts FIRST; high-coeff target the attacker wants
	bpCheap = "zzz-cheap/Model" // sorts last; what the attacker actually proves
)

func bpModels() []types.Model {
	return []types.Model{
		{ProposedBy: "genesis", Id: bpRich, VRam: 96},
		{ProposedBy: "genesis", Id: bpCheap, VRam: 64},
	}
}

func bpKeeper(addr string, declared ...string) *mockKeeperForModelAssigner {
	return &mockKeeperForModelAssigner{
		governanceModels: bpModels(),
		hardwareNodes: map[string]*types.HardwareNodes{
			addr: {Participant: addr, HardwareNodes: []*types.HardwareNode{
				{LocalId: "n1", Models: declared},
			}},
		},
	}
}

// helper: which model bucket is node n1 seated in after seating?
func seatOf(p *types.ActiveParticipant, nodeId string) string {
	for i, m := range p.Models {
		if i >= len(p.MlNodes) || p.MlNodes[i] == nil {
			continue
		}
		for _, nd := range p.MlNodes[i].MlNodes {
			if nd.NodeId == nodeId {
				return m
			}
		}
	}
	return "<dropped>"
}

// Vector 1: prove cheap, relabel hardware to rich only. Must DROP, never seat rich.
func TestBypass_RelabelToRichOnly(t *testing.T) {
	addr := "gonka1v1"
	k := bpKeeper(addr, bpRich) // hardware declares only the rich model
	p := &types.ActiveParticipant{
		Index:   addr,
		Models:  []string{bpCheap},
		MlNodes: []*types.ModelMLNodes{{MlNodes: []*types.MLNodeInfo{{NodeId: "n1", PocWeight: 1000}}}},
	}
	NewModelAssigner(k, mockLogger{}).setModelsForParticipants(context.Background(), []*types.ActiveParticipant{p}, types.Epoch{Index: 2})
	require.Equal(t, "<dropped>", seatOf(p, "n1"), "BYPASS: relabel moved weight to a model it never proved")
}

// Vector 2: declare BOTH on hardware (rich sorts first). Must stay cheap, never rich.
func TestBypass_DeclareBothRichFirst(t *testing.T) {
	addr := "gonka1v2"
	k := bpKeeper(addr, bpRich, bpCheap)
	p := &types.ActiveParticipant{
		Index:   addr,
		Models:  []string{bpCheap},
		MlNodes: []*types.ModelMLNodes{{MlNodes: []*types.MLNodeInfo{{NodeId: "n1", PocWeight: 1000}}}},
	}
	NewModelAssigner(k, mockLogger{}).setModelsForParticipants(context.Background(), []*types.ActiveParticipant{p}, types.Epoch{Index: 2})
	require.Equal(t, bpCheap, seatOf(p, "n1"), "BYPASS: node seated in rich model despite proving only cheap")
}

// Vector 3: AMPLIFIER — if a node is validated under BOTH models with higher weight
// on the rich one, the single-assignment rule seats it rich. This is only reachable
// if the attacker already has a validated rich bucket (forged provenance upstream).
// Documents that the fix's guarantee == validation integrity.
func TestBypass_DualBucketHigherWeightWinsRich(t *testing.T) {
	addr := "gonka1v3"
	k := bpKeeper(addr, bpRich, bpCheap)
	p := &types.ActiveParticipant{
		Index:  addr,
		Models: []string{bpRich, bpCheap},
		MlNodes: []*types.ModelMLNodes{
			{MlNodes: []*types.MLNodeInfo{{NodeId: "n1", PocWeight: 999}}}, // rich claim, higher
			{MlNodes: []*types.MLNodeInfo{{NodeId: "n1", PocWeight: 10}}},  // cheap claim, real
		},
	}
	NewModelAssigner(k, mockLogger{}).setModelsForParticipants(context.Background(), []*types.ActiveParticipant{p}, types.Epoch{Index: 2})
	got := seatOf(p, "n1")
	t.Logf("dual-bucket seat = %s (rich wins iff a forged rich bucket exists)", got)
	require.Equal(t, bpRich, got, "expected higher-weight rich claim to win once it exists")
}

// Vector 4: two nodes — relabel one to rich, keep the other cheap. Rich node must drop;
// the rich subgroup must not gain the attacker.
func TestBypass_SplitRelabelKeepsValidatorWeight(t *testing.T) {
	addr := "gonka1v4"
	k := &mockKeeperForModelAssigner{
		governanceModels: bpModels(),
		hardwareNodes: map[string]*types.HardwareNodes{
			addr: {Participant: addr, HardwareNodes: []*types.HardwareNode{
				{LocalId: "n1", Models: []string{bpRich}},  // relabeled
				{LocalId: "n2", Models: []string{bpCheap}}, // honest
			}},
		},
	}
	p := &types.ActiveParticipant{
		Index:  addr,
		Models: []string{bpCheap},
		MlNodes: []*types.ModelMLNodes{{MlNodes: []*types.MLNodeInfo{
			{NodeId: "n1", PocWeight: 1000},
			{NodeId: "n2", PocWeight: 500},
		}}},
	}
	NewModelAssigner(k, mockLogger{}).setModelsForParticipants(context.Background(), []*types.ActiveParticipant{p}, types.Epoch{Index: 2})
	require.Equal(t, "<dropped>", seatOf(p, "n1"), "BYPASS: relabeled node reached rich model")
	require.Equal(t, bpCheap, seatOf(p, "n2"), "honest node should stay")
	require.NotContains(t, p.Models, bpRich, "BYPASS: rich bucket created from a hardware claim")
}
