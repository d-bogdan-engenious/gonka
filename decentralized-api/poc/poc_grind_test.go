package poc

import (
	"context"
	"errors"
	"testing"
	"time"

	"decentralized-api/broker"
	"decentralized-api/cosmosclient"
	"decentralized-api/mlnodeclient"
	"decentralized-api/poc/earlyshare"

	"github.com/productscience/inference/x/inference/types"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// TestRetryBackoffDelay_DefaultsAndClamp covers the two guard branches: a
// non-positive base falls back to the default RetryBackoff, and a negative
// attempt is clamped to 0.
func TestRetryBackoffDelay_DefaultsAndClamp(t *testing.T) {
	// base <= 0 -> default base (3s), attempt 0 -> 1.5^0 = base.
	require.Equal(t, DefaultValidationConfig().RetryBackoff, retryBackoffDelay(0, 0))
	require.Equal(t, DefaultValidationConfig().RetryBackoff, retryBackoffDelay(-5*time.Second, 0))
	// attempt < 0 -> clamped to 0, so same as attempt 0 for a fixed base.
	base := 3 * time.Second
	require.Equal(t, retryBackoffDelay(base, 0), retryBackoffDelay(base, -1))
	require.Equal(t, base, retryBackoffDelay(base, -1))
}

// TestCheckValidateeProofs_DuplicateNonces: a response that repeats a nonce is
// fraud and must vote -1 (permanent), not retry.
func TestCheckValidateeProofs_DuplicateNonces(t *testing.T) {
	v := &OffChainValidator{pubKey: "pk"}
	work := &participantWork{address: "a", modelId: "test-model", count: 100}
	fetcher := &stubProofFetcher{artifacts: []VerifiedArtifact{{Nonce: 5}, {Nonce: 5}}}

	failure, passed := v.checkValidateeProofs(context.Background(), work, fetcher, 1000, "hash", 10, nil)
	require.False(t, passed)
	require.Equal(t, validateFailPermanent, failure)
}

// TestCheckValidateeProofs_PorosityTooHigh: a max nonce far above the committed
// count is fraud and must vote -1 (permanent).
func TestCheckValidateeProofs_PorosityTooHigh(t *testing.T) {
	v := &OffChainValidator{pubKey: "pk"}
	// porosity = 10000 / 50 = 200 >= porosityThreshold (100).
	work := &participantWork{address: "a", modelId: "test-model", count: 50}
	fetcher := &stubProofFetcher{artifacts: []VerifiedArtifact{{Nonce: 10000}}}

	failure, passed := v.checkValidateeProofs(context.Background(), work, fetcher, 1000, "hash", 10, nil)
	require.False(t, passed)
	require.Equal(t, validateFailPermanent, failure)
}

// TestDispatchToMLNode_MissingModelConfig: a committed model with no PoC params
// config is not the validatee's fault, so we abstain (no vote).
func TestDispatchToMLNode_MissingModelConfig(t *testing.T) {
	v := &OffChainValidator{
		nodeBroker:  &stubNodeBroker{client: mlnodeclient.NewMockClient()},
		callbackUrl: "http://cb",
	}
	work := &participantWork{address: "a", modelId: "absent-model", verified: []VerifiedArtifact{{Nonce: 1}}}
	nodeCounter := 0

	// testModelPocParams only configures "test-model", so "absent-model" has no config.
	result := v.dispatchToMLNode(context.Background(), work,
		[]broker.NodeResponse{nodeForModel("absent-model")}, &nodeCounter,
		1000, "start-hash", testModelPocParams())

	require.Equal(t, validateAbstain, result)
}

// TestDispatchToMLNode_DecodeWithoutSchemeBlock: a DECODE stage whose model config
// carries no DECODE scheme block falls back to seqLen=0, still dispatches, and
// sends decode-packed artifacts (KPointsSteps) rather than a vector blob.
func TestDispatchToMLNode_DecodeWithoutSchemeBlock(t *testing.T) {
	mockClient := mlnodeclient.NewMockClient()
	v := &OffChainValidator{
		nodeBroker:  &stubNodeBroker{client: mockClient},
		callbackUrl: "http://cb",
	}
	pocParams := &types.PocParams{
		PocScheme: types.PocScheme_POC_SCHEME_DECODE,
		Models: []*types.PoCModelConfig{
			{
				ModelId:           "test-model",
				SeqLen:            256,
				StatTest:          types.DefaultPoCStatTestParams(),
				WeightScaleFactor: types.DecimalFromFloat(1.0),
				// No Schemes block -> SchemeParams(DECODE) returns (nil, false).
			},
		},
	}
	work := &participantWork{
		address: "a", modelId: "test-model", pubKey: "pk",
		verified: []VerifiedArtifact{{Nonce: 1, VectorB64: ""}},
	}
	nodeCounter := 0

	result := v.dispatchToMLNode(context.Background(), work,
		[]broker.NodeResponse{nodeForModel("test-model")}, &nodeCounter,
		1000, "start-hash", pocParams)

	require.Equal(t, validateDispatched, result)

	mockClient.Mu.Lock()
	defer mockClient.Mu.Unlock()
	require.NotNil(t, mockClient.LastGenerateV2Req)
	require.True(t, mockClient.LastGenerateV2Req.Params.Decode)
	require.Equal(t, int64(0), mockClient.LastGenerateV2Req.Params.SeqLen)
}

// TestFilterNodesForValidation_StatusMatrix exercises every skip/accept branch of
// the node filter in one pass.
func TestFilterNodesForValidation_StatusMatrix(t *testing.T) {
	mkNode := func(id string, status types.HardwareNodeStatus, operational, preserved bool) broker.NodeResponse {
		ns := broker.NodeState{CurrentStatus: status}
		if operational {
			ns.AdminState = broker.AdminState{Enabled: true, Epoch: 0}
		} else {
			ns.AdminState = broker.AdminState{Enabled: false, Epoch: 0}
		}
		if preserved {
			ns.PreservedModels = map[string]bool{"m": true}
		}
		return broker.NodeResponse{Node: broker.Node{Id: id}, State: ns}
	}

	nodes := []broker.NodeResponse{
		mkNode("failed", types.HardwareNodeStatus_FAILED, true, false),       // skip: FAILED
		mkNode("unknown", types.HardwareNodeStatus_UNKNOWN, true, false),     // skip: UNKNOWN
		mkNode("not-op", types.HardwareNodeStatus_POC, false, false),         // skip: non-operational
		mkNode("preserved", types.HardwareNodeStatus_POC, true, true),        // skip: preserved for inference
		mkNode("stopped", types.HardwareNodeStatus_STOPPED, true, false),     // skip: unhandled status
		mkNode("poc", types.HardwareNodeStatus_POC, true, false),             // accept: POC
		mkNode("inference", types.HardwareNodeStatus_INFERENCE, true, false), // accept: INFERENCE
	}

	// latestEpoch 5 > adminState.Epoch 0 with Enabled -> operational nodes pass that gate.
	filtered := filterNodesForValidation(nodes, 5, types.InferencePhase, false)

	ids := make([]string, 0, len(filtered))
	for _, n := range filtered {
		ids = append(ids, n.Node.Id)
	}
	require.ElementsMatch(t, []string{"poc", "inference"}, ids)
}

// TestReportInvalidParticipant_ChallengeVote: when the validatee is the open
// challenge target at this stage, the -1 goes out as a challenge validation.
func TestReportInvalidParticipant_ChallengeVote(t *testing.T) {
	t.Cleanup(OpenChallenges.Reset)
	OpenChallenges.Replace("", []*types.OpenPoCChallenge{openCh("gonka1bad", 1000, 2000, true)}, 0)

	recorder := &cosmosclient.MockCosmosMessageClient{}
	recorder.On("SubmitPoCChallengeValidations", mock.MatchedBy(func(msg *types.MsgSubmitPoCChallengeValidations) bool {
		return msg.PocStageStartBlockHeight == 1000 &&
			len(msg.Validations) == 1 &&
			msg.Validations[0].ParticipantAddress == "gonka1bad" &&
			msg.Validations[0].ValidatedWeight == -1
	})).Return(nil).Once()

	v := &OffChainValidator{recorder: recorder}
	v.reportInvalidParticipant(1000, "gonka1bad", "model")

	recorder.AssertExpectations(t)
	recorder.AssertNotCalled(t, "SubmitPocValidationsV2", mock.Anything)
}

// TestReportInvalidParticipant_ChallengeVoteSubmitError covers the error-log branch
// when the challenge submission itself fails.
func TestReportInvalidParticipant_ChallengeVoteSubmitError(t *testing.T) {
	t.Cleanup(OpenChallenges.Reset)
	OpenChallenges.Replace("", []*types.OpenPoCChallenge{openCh("gonka1bad", 1000, 2000, true)}, 0)

	recorder := &cosmosclient.MockCosmosMessageClient{}
	recorder.On("SubmitPoCChallengeValidations", mock.Anything).Return(errors.New("submit failed")).Once()

	v := &OffChainValidator{recorder: recorder}
	v.reportInvalidParticipant(1000, "gonka1bad", "model")

	recorder.AssertExpectations(t)
}

// TestReportInvalidParticipant_RegularVoteSubmitError covers the error-log branch of
// the ordinary (non-challenge) vote path.
func TestReportInvalidParticipant_RegularVoteSubmitError(t *testing.T) {
	t.Cleanup(OpenChallenges.Reset)
	OpenChallenges.Reset() // no open challenge -> VoteFor returns nil -> regular path.

	recorder := &cosmosclient.MockCosmosMessageClient{}
	recorder.On("SubmitPocValidationsV2", mock.Anything).Return(errors.New("submit failed")).Once()

	v := &OffChainValidator{recorder: recorder}
	v.reportInvalidParticipant(1000, "gonka1other", "model")

	recorder.AssertExpectations(t)
	recorder.AssertNotCalled(t, "SubmitPoCChallengeValidations", mock.Anything)
}

// TestCheckValidateeProofs_EarlyGuardArms drives the early-share guard switch arms
// inside checkValidateeProofs. decide's own logic is tested in TestDecide; here we
// pin how each outcome maps to a validateResult in enforce vs observe mode.
func TestCheckValidateeProofs_EarlyGuardArms(t *testing.T) {
	ctx := context.Background()
	finalRoot := []byte{0xaa}
	earlyRoot := []byte{0xee}

	type rootEntry = struct {
		arts []VerifiedArtifact
		err  error
	}
	// mkFetcher wires: the phase-1 main fetch (by final root), the early-guard
	// inclusion fetch (by early root), and the early-guard final by-nonce fetch.
	mkFetcher := func(earlyArt, finalByNonceArt VerifiedArtifact, earlyErr, finalErr error) *scriptedProofFetcher {
		f := &scriptedProofFetcher{byRoot: map[string]rootEntry{}, byNonce: map[string]rootEntry{}}
		f.byRoot[string(finalRoot)] = rootEntry{arts: []VerifiedArtifact{{LeafIndex: 0, Nonce: 1, VectorB64: "main"}}}
		f.byRoot[string(earlyRoot)] = rootEntry{arts: []VerifiedArtifact{earlyArt}, err: earlyErr}
		f.byNonce[string(finalRoot)] = rootEntry{arts: []VerifiedArtifact{finalByNonceArt}, err: finalErr}
		return f
	}
	baseDec := earlyDecision{requireInclusion: true, earlyCount: 10, earlyRoot: earlyRoot, finalCount: 100, earlyShare: 0.1, threshold: 0.05}
	mkGR := func(guard *EarlyShareGuard) *guardRuntime {
		return &guardRuntime{
			guard:             guard,
			decisions:         map[string]earlyDecision{earlyShareKey("a", "m1"): baseDec},
			stage:             1000,
			validatorPubKey:   "pub",
			samplingBlockHash: "hash",
		}
	}
	newWork := func() *participantWork {
		return &participantWork{address: "a", modelId: "m1", count: 100, url: "http://p", rootHash: finalRoot}
	}
	guardFor := func(mode earlyshare.Mode) *EarlyShareGuard {
		g := NewEarlyShareGuard(earlyshare.Config{Mode: mode, RequireInclusionProof: true, InclusionSampleSize: 1}, newFakeStore())
		require.NotNil(t, g)
		return g
	}
	art := VerifiedArtifact{LeafIndex: 3, Nonce: 7, VectorB64: "vec"}

	v := &OffChainValidator{pubKey: "pub"}

	t.Run("vote-no enforce -> fail permanent", func(t *testing.T) {
		f := mkFetcher(
			VerifiedArtifact{LeafIndex: 3, Nonce: 7, VectorB64: "early-vec"},
			VerifiedArtifact{LeafIndex: 9, Nonce: 7, VectorB64: "final-vec"},
			nil, nil)
		failure, passed := v.checkValidateeProofs(ctx, newWork(), f, 1000, "hash", 1, mkGR(guardFor(earlyshare.ModeEnforce)))
		require.False(t, passed)
		require.Equal(t, validateFailPermanent, failure)
	})

	t.Run("retry enforce -> fail retry", func(t *testing.T) {
		f := mkFetcher(art, art, context.DeadlineExceeded, nil)
		failure, passed := v.checkValidateeProofs(ctx, newWork(), f, 1000, "hash", 1, mkGR(guardFor(earlyshare.ModeEnforce)))
		require.False(t, passed)
		require.Equal(t, validateFailRetry, failure)
	})

	t.Run("vote-no observe -> passes through", func(t *testing.T) {
		f := mkFetcher(
			VerifiedArtifact{LeafIndex: 3, Nonce: 7, VectorB64: "early-vec"},
			VerifiedArtifact{LeafIndex: 9, Nonce: 7, VectorB64: "final-vec"},
			nil, nil)
		_, passed := v.checkValidateeProofs(ctx, newWork(), f, 1000, "hash", 1, mkGR(guardFor(earlyshare.ModeObserve)))
		require.True(t, passed, "observe mode only logs, it never blocks")
	})

	t.Run("retry observe -> passes through", func(t *testing.T) {
		f := mkFetcher(art, art, context.DeadlineExceeded, nil)
		_, passed := v.checkValidateeProofs(ctx, newWork(), f, 1000, "hash", 1, mkGR(guardFor(earlyshare.ModeObserve)))
		require.True(t, passed)
	})

	t.Run("abstain-retry observe -> passes through", func(t *testing.T) {
		gr := mkGR(guardFor(earlyshare.ModeObserve))
		gr.decisions[earlyShareKey("a", "m1")] = earlyDecision{
			shareVoteNo: true, requireInclusion: true, earlyCount: 10, earlyRoot: earlyRoot,
			finalCount: 100, earlyShare: 0.1, threshold: 0.05,
		}
		wrapped := errLocalFailure()
		f := mkFetcher(art, art, wrapped, nil)
		_, passed := v.checkValidateeProofs(ctx, newWork(), f, 1000, "hash", 1, gr)
		require.True(t, passed)
	})
}

// errLocalFailure returns an ErrLocalRequestFailure-wrapped error, matching the
// "we never reached the validatee" classification the guard abstains on.
func errLocalFailure() error {
	return errors.Join(ErrLocalRequestFailure, errors.New("failed to sign request: keyring locked"))
}
