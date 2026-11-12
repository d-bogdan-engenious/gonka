package broker

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"decentralized-api/apiconfig"
	"decentralized-api/chainphase"
	"decentralized-api/mlnodeclient"

	"github.com/productscience/inference/x/inference/types"
	"github.com/stretchr/testify/require"
)

// closedDBConfig returns a ConfigManager whose SQLite handle is already closed, so
// every KV read/write through it fails. Used to drive the DB-error branches.
func closedDBConfig(t *testing.T) *apiconfig.ConfigManager {
	t.Helper()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte("{}"), 0600))
	config, err := apiconfig.LoadConfigManagerWithPaths(configPath, filepath.Join(dir, "config.db"), "")
	require.NoError(t, err)
	require.NoError(t, config.SqlDb().GetDb().Close())
	return config
}

func modelCache() map[string]apiconfig.PoCModelConfigCache {
	return map[string]apiconfig.PoCModelConfigCache{"model-a": {ModelId: "model-a"}}
}

func nodeServingModelA(id string) *NodeWithState {
	return &NodeWithState{Node: Node{Id: id, Models: map[string]ModelArgs{"model-a": {}}}}
}

// ---------------------------------------------------------------------------
// activePoCGenerationHeight
// ---------------------------------------------------------------------------

func TestActivePoCGenerationHeight_NilNotSyncedAndWindDown(t *testing.T) {
	// Guarantee no challenge overlay steers the result.
	withOverlay(t, stubChallengeOverlay{self: "none"})

	h, ok := activePoCGenerationHeight(nil)
	require.False(t, ok)
	require.Zero(t, h)

	h, ok = activePoCGenerationHeight(&chainphase.EpochState{IsSynced: false})
	require.False(t, ok)
	require.Zero(t, h)

	epochParams := types.EpochParams{PocStageDuration: 100, PocExchangeDuration: 50}
	state := &chainphase.EpochState{
		LatestEpoch:  types.NewEpochContext(types.Epoch{Index: 1, PocStartBlockHeight: 100}, epochParams),
		CurrentBlock: chainphase.BlockInfo{Height: 110},
		CurrentPhase: types.PoCGenerateWindDownPhase,
		IsSynced:     true,
	}
	h, ok = activePoCGenerationHeight(state)
	require.True(t, ok, "wind-down inside the PoC exchange window is still active generation")
	require.Equal(t, int64(100), h)

	// Wind-down far past the exchange window is no longer active.
	state.CurrentBlock = chainphase.BlockInfo{Height: 100000}
	h, ok = activePoCGenerationHeight(state)
	require.False(t, ok)
	require.Zero(t, h)
}

// ---------------------------------------------------------------------------
// assignPoCNodeNumber (non-DB branches)
// ---------------------------------------------------------------------------

func TestAssignPoCNodeNumber_NilParams(t *testing.T) {
	b := &Broker{}
	cmd := StartPoCNodeCommandV2{Model: "model-a"}
	err := b.assignPoCNodeNumber(context.Background(), "node-1", nil, &cmd)
	require.Error(t, err)
	require.Contains(t, err.Error(), "missing PoC parameters")
}

func TestAssignPoCNodeNumber_NoAllocationForModel(t *testing.T) {
	b := &Broker{
		nodes:          map[string]*NodeWithState{"node-1": nodeServingModelA("node-1")},
		pocNodeNumbers: &pocNodeNumbers{Rollout: &pocNodeNumberRollout{Ready: true}},
	}
	params := &pocParams{startPoCBlockHeight: 100, models: modelCache()}
	// No node serves "model-z", so there is no allocation for it at this height.
	cmd := StartPoCNodeCommandV2{Model: "model-z"}
	err := b.assignPoCNodeNumber(context.Background(), "node-1", params, &cmd)
	require.Error(t, err)
	require.Contains(t, err.Error(), "no PoC allocation")
}

// A v0.2.16-post2 record has a mapping but no rollout marker. A direct assign
// then adopts the current stage as legacy and defers while that stage's mapping
// is still ambiguous.
func TestAssignPoCNodeNumber_Post2RecordWithoutRolloutDefers(t *testing.T) {
	b := &Broker{
		nodes: map[string]*NodeWithState{"node-1": nodeServingModelA("node-1")},
		pocNodeNumbers: &pocNodeNumbers{
			Height: 100,
			Models: map[string][]string{"model-a": {"node-1"}},
			// Rollout intentionally nil.
		},
	}
	params := &pocParams{startPoCBlockHeight: 100, models: modelCache()}
	cmd := StartPoCNodeCommandV2{Model: "model-a"}
	err := b.assignPoCNodeNumber(context.Background(), "node-1", params, &cmd)
	require.ErrorIs(t, err, errPoCNodeNumberRolloutStage)
}

// ---------------------------------------------------------------------------
// DB-error branches (closed SQLite handle)
// ---------------------------------------------------------------------------

func TestPoCNodeNumbers_DBErrorBranches(t *testing.T) {
	ctx := context.Background()

	t.Run("loadPoCNodeNumbers surfaces KV read errors", func(t *testing.T) {
		b := &Broker{configManager: closedDBConfig(t)}
		require.Error(t, b.loadPoCNodeNumbers(ctx))
	})

	t.Run("savePoCNodeNumbers surfaces KV write errors", func(t *testing.T) {
		b := &Broker{configManager: closedDBConfig(t)}
		require.Error(t, b.savePoCNodeNumbers(ctx, &pocNodeNumbers{}))
	})

	t.Run("assignPoCNodeNumber propagates a load error", func(t *testing.T) {
		b := &Broker{configManager: closedDBConfig(t), nodes: map[string]*NodeWithState{}}
		params := &pocParams{startPoCBlockHeight: 100, models: modelCache()}
		cmd := StartPoCNodeCommandV2{Model: "model-a"}
		require.Error(t, b.assignPoCNodeNumber(ctx, "node-1", params, &cmd))
	})

	t.Run("assignPoCNodeNumber propagates a fallback save error", func(t *testing.T) {
		b := &Broker{
			configManager: closedDBConfig(t),
			nodes:         map[string]*NodeWithState{"node-1": nodeServingModelA("node-1")},
			// pocNodeNumbers preset so load returns early; Rollout nil forces the fallback save.
			pocNodeNumbers: &pocNodeNumbers{Height: 100, Models: map[string][]string{"model-a": {"node-1"}}},
		}
		params := &pocParams{startPoCBlockHeight: 100, models: modelCache()}
		cmd := StartPoCNodeCommandV2{Model: "model-a"}
		require.Error(t, b.assignPoCNodeNumber(ctx, "node-1", params, &cmd))
	})

	t.Run("assignPoCNodeNumber propagates a mapping save error", func(t *testing.T) {
		b := &Broker{
			configManager: closedDBConfig(t),
			nodes:         map[string]*NodeWithState{"node-1": nodeServingModelA("node-1")},
			// Ready rollout at a different height forces a fresh mapping build + save.
			pocNodeNumbers: &pocNodeNumbers{Height: 99, Rollout: &pocNodeNumberRollout{Ready: true}},
		}
		params := &pocParams{startPoCBlockHeight: 100, models: modelCache()}
		cmd := StartPoCNodeCommandV2{Model: "model-a"}
		require.Error(t, b.assignPoCNodeNumber(ctx, "node-1", params, &cmd))
	})

	t.Run("PoCNodeID propagates a load error", func(t *testing.T) {
		b := &Broker{configManager: closedDBConfig(t)}
		_, err := b.PoCNodeID(ctx, 100, "model-a", 0)
		require.Error(t, err)
	})

	t.Run("preparePoCNodeNumberingRollout propagates a load error", func(t *testing.T) {
		b := &Broker{configManager: closedDBConfig(t)}
		err := b.preparePoCNodeNumberingRollout(ctx, &chainphase.EpochState{IsSynced: true})
		require.Error(t, err)
	})
}

// ---------------------------------------------------------------------------
// node_worker_commands.go
// ---------------------------------------------------------------------------

// grindClient overrides individual MLNode calls to inject transport errors or a
// nil status response without changing the shared MockClient semantics.
type grindClient struct {
	*mlnodeclient.MockClient
	stopErr   error
	initErr   error
	nilStatus bool
}

func (c *grindClient) StopPowV2(ctx context.Context) (*mlnodeclient.PoCStopResponseV2, error) {
	if c.stopErr != nil {
		return nil, c.stopErr
	}
	return c.MockClient.StopPowV2(ctx)
}

func (c *grindClient) InitGenerateV2(ctx context.Context, req mlnodeclient.PoCInitGenerateRequestV2) (*mlnodeclient.PoCInitGenerateResponseV2, error) {
	if c.initErr != nil {
		return nil, c.initErr
	}
	return c.MockClient.InitGenerateV2(ctx, req)
}

func (c *grindClient) GetPowStatusV2(ctx context.Context) (*mlnodeclient.PoCStatusResponseV2, error) {
	if c.nilStatus {
		return nil, nil
	}
	return c.MockClient.GetPowStatusV2(ctx)
}

func workerWithClient(client mlnodeclient.MLNodeClient) *NodeWorker {
	return &NodeWorker{
		nodeId:      "grind-node",
		node:        createTestNode("grind-node"),
		getClientFn: func(*NodeWithState) mlnodeclient.MLNodeClient { return client },
	}
}

func TestSetChallengeCommitLeadBlocks(t *testing.T) {
	orig := ChallengeCommitLeadBlocksValue()
	t.Cleanup(func() { challengeCommitLeadBlocks = orig })

	SetChallengeCommitLeadBlocks(9)
	require.Equal(t, int64(9), ChallengeCommitLeadBlocksValue())
	// Non-positive values are ignored.
	SetChallengeCommitLeadBlocks(0)
	require.Equal(t, int64(9), ChallengeCommitLeadBlocksValue())
	SetChallengeCommitLeadBlocks(-3)
	require.Equal(t, int64(9), ChallengeCommitLeadBlocksValue())
}

func TestSelectInferenceModel_GovernanceFallback(t *testing.T) {
	newWorker := func(bridge *MockBrokerChainBridge) *NodeWorker {
		node := createTestNode("gov-node")
		node.Node.Models = map[string]ModelArgs{"gov-model": {}}
		node.State.EpochMLNodes = map[string]types.MLNodeInfo{"gov-model": {NodeId: "gov-node"}}
		node.State.EpochModels = map[string]types.Model{} // not locally resolved -> governance lookup
		return &NodeWorker{nodeId: "gov-node", node: node, broker: &Broker{chainBridge: bridge}}
	}

	t.Run("governance query error", func(t *testing.T) {
		bridge := &MockBrokerChainBridge{}
		bridge.On("GetGovernanceModels").Return(nil, errors.New("rpc down")).Once()
		_, err := selectInferenceModel(newWorker(bridge))
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to get governance models")
	})

	t.Run("governance model found", func(t *testing.T) {
		bridge := &MockBrokerChainBridge{}
		bridge.On("GetGovernanceModels").Return(&types.QueryModelsAllResponse{
			Model: []types.Model{{Id: "other"}, {Id: "gov-model"}},
		}, nil).Once()
		model, err := selectInferenceModel(newWorker(bridge))
		require.NoError(t, err)
		require.Equal(t, "gov-model", model.Id)
	})

	t.Run("governance model missing", func(t *testing.T) {
		bridge := &MockBrokerChainBridge{}
		bridge.On("GetGovernanceModels").Return(&types.QueryModelsAllResponse{
			Model: []types.Model{{Id: "other"}},
		}, nil).Once()
		_, err := selectInferenceModel(newWorker(bridge))
		require.Error(t, err)
		require.Contains(t, err.Error(), "no epoch models available")
	})
}

func TestStopPowV2Checked_TransportError(t *testing.T) {
	client := &grindClient{MockClient: mlnodeclient.NewMockClient(), stopErr: errors.New("conn refused")}
	err := stopPowV2Checked(context.Background(), workerWithClient(client))
	require.Error(t, err)
}

func TestStopPowV2Tolerant_Branches(t *testing.T) {
	t.Run("transport error", func(t *testing.T) {
		client := &grindClient{MockClient: mlnodeclient.NewMockClient(), stopErr: errors.New("conn refused")}
		require.Error(t, stopPowV2Tolerant(context.Background(), workerWithClient(client)))
	})

	t.Run("all backends failed is an error", func(t *testing.T) {
		mc := mlnodeclient.NewMockClient()
		mc.StopPowV2Resp = &mlnodeclient.PoCStopResponseV2{
			Status: "OK",
			Errors: []mlnodeclient.BackendError{{Port: 5001, Error: "500"}},
			// No Results -> nothing stopped -> hard error.
		}
		require.Error(t, stopPowV2Tolerant(context.Background(), workerWithClient(mc)))
	})
}

func TestInitGenerateV2Checked_TransportError(t *testing.T) {
	client := &grindClient{MockClient: mlnodeclient.NewMockClient(), initErr: errors.New("conn refused")}
	err := initGenerateV2Checked(context.Background(), workerWithClient(client), mlnodeclient.PoCInitGenerateRequestV2{})
	require.Error(t, err)
}

func TestExecuteWindDown_NilStatusWithoutError(t *testing.T) {
	client := &grindClient{MockClient: mlnodeclient.NewMockClient(), nilStatus: true}
	cmd := StartPoCNodeCommandV2{WindDown: true, BlockHeight: 500, BlockHash: "h"}
	result := cmd.executeWindDown(context.Background(), workerWithClient(client), NodeResult{})
	require.False(t, result.Succeeded)
	require.Contains(t, result.Error, "nil during wind-down")
}
