package epochgroup

import (
	"context"
	"testing"

	"github.com/cosmos/cosmos-sdk/types/query"
	"github.com/cosmos/cosmos-sdk/x/group"
	"github.com/productscience/inference/x/inference/types"
	"github.com/stretchr/testify/require"
)

// Branch coverage for the unit-of-compute PRICE mechanism (executor pay rate),
// which was 0% covered. One assertion per decision branch — this is the live
// "price per epoch" path, exercised manually on tn3 each epoch.

func TestWeightedMedian_Branches(t *testing.T) {
	mk := func(price uint64, weight int64) *weightedProposal {
		return &weightedProposal{Participant: "p", Price: price, Weight: weight}
	}
	tests := []struct {
		name  string
		in    []*weightedProposal
		want  uint64
	}{
		{"empty -> 0", nil, 0},
		{"all-zero-weight -> 0", []*weightedProposal{mk(10, 0), mk(20, 0)}, 0},
		{"negative total weight -> 0", []*weightedProposal{mk(10, -5), mk(20, -5)}, 0},
		{"single proposal -> its price", []*weightedProposal{mk(77, 5)}, 77},
		{"uniform weights, odd crossing -> middle", []*weightedProposal{mk(30, 1), mk(10, 1), mk(20, 1)}, 20},
		{"even, exact half crossing -> lower", []*weightedProposal{mk(20, 1), mk(10, 1)}, 10},
		{"low-price majority weight dominates", []*weightedProposal{mk(10, 10), mk(20, 1), mk(30, 1)}, 10},
		{"high-price majority weight dominates", []*weightedProposal{mk(10, 1), mk(20, 1), mk(30, 10)}, 30},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, weightedMedian(tc.in))
		})
	}
}

func TestGetWeight_Branches(t *testing.T) {
	require.Equal(t, int64(0), getWeight(nil), "nil participant")
	require.Equal(t, int64(0), getWeight(&group.GroupMember{Member: nil}), "nil member")
	require.Equal(t, int64(0), getWeight(&group.GroupMember{Member: &group.Member{Weight: "not-a-number"}}), "unparseable weight")
	require.Equal(t, int64(42), getWeight(&group.GroupMember{Member: &group.Member{Weight: "42"}}), "valid weight")
}

func TestComputeUnitOfComputePrice_DefaultFallbackAndMedian(t *testing.T) {
	// Two equal-weight members: one proposes 50, the other submitted nothing and
	// must fall back to the default (200). Weighted median across {50,200} at
	// equal weight crosses at the lower value -> 50. Exercises the no-proposal
	// default branch AND the median path.
	members := []*group.GroupMember{
		{Member: &group.Member{Address: "proposer", Weight: "100"}},
		{Member: &group.Member{Address: "silent", Weight: "100"}},
	}
	eg := &EpochGroup{
		GroupKeeper: &mockGroupKeeper{
			fn: func(ctx context.Context, req *group.QueryGroupMembersRequest) (*group.QueryGroupMembersResponse, error) {
				return &group.QueryGroupMembersResponse{Members: members, Pagination: &query.PageResponse{NextKey: nil}}, nil
			},
		},
		Logger:    &mockLogger{},
		GroupData: &types.EpochGroupData{EpochGroupId: 1},
	}

	proposals := []*types.UnitOfComputePriceProposal{
		{Participant: "proposer", Price: 50},
	}
	got, err := eg.ComputeUnitOfComputePrice(context.Background(), proposals, 200)
	require.NoError(t, err)
	require.Equal(t, uint64(50), got)
}

func TestComputeUnitOfComputePrice_AllDefaultWhenNoProposals(t *testing.T) {
	// No proposals at all -> every member uses the default -> median == default.
	members := []*group.GroupMember{
		{Member: &group.Member{Address: "a", Weight: "100"}},
		{Member: &group.Member{Address: "b", Weight: "100"}},
		{Member: &group.Member{Address: "c", Weight: "100"}},
	}
	eg := &EpochGroup{
		GroupKeeper: &mockGroupKeeper{
			fn: func(ctx context.Context, req *group.QueryGroupMembersRequest) (*group.QueryGroupMembersResponse, error) {
				return &group.QueryGroupMembersResponse{Members: members, Pagination: &query.PageResponse{NextKey: nil}}, nil
			},
		},
		Logger:    &mockLogger{},
		GroupData: &types.EpochGroupData{EpochGroupId: 1},
	}
	got, err := eg.ComputeUnitOfComputePrice(context.Background(), nil, 123)
	require.NoError(t, err)
	require.Equal(t, uint64(123), got)
}
