package poc

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"decentralized-api/cosmosclient"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// Error-path coverage for FetchAndVerifyProofs (was 65.9%). These branches decide a
// validator's PoC vote: a non-200 from the validatee is a validatee-side transient
// (worker retries, then votes -1), and a local signing failure must surface as
// ErrLocalRequestFailure so the worker ABSTAINS (never a -1 on our own fault).

// Non-200 from the validatee's /v1/poc/proofs → error mentioning the status.
func TestFetchAndVerifyProofs_Non200Status(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprint(w, "node unavailable")
	}))
	defer srv.Close()

	client := &ProofClient{httpClient: srv.Client(), recorder: bodyCapRecorder()}
	_, err := client.FetchAndVerifyProofs(context.Background(), srv.URL, ProofRequest{
		Count: 1, TreeDepth: 12, LeafIndices: []uint32{0}, ParticipantAddress: "victim",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "status 503")
}

// A local signing failure (broken keyring) must be classified as ErrLocalRequestFailure
// — the request never left our side, so the validator learned nothing about the validatee.
func TestFetchAndVerifyProofs_LocalSignFailureAbstains(t *testing.T) {
	r := &cosmosclient.MockCosmosMessageClient{}
	r.On("GetAccountAddress").Return("validator-address")
	r.On("GetSignerAddress").Return("validator-signer")
	r.On("SignBytes", mock.Anything).Return([]byte(nil), fmt.Errorf("keyring unavailable"))

	client := &ProofClient{httpClient: http.DefaultClient, recorder: r}
	_, err := client.FetchAndVerifyProofs(context.Background(), "http://unused.invalid", ProofRequest{
		Count: 1, TreeDepth: 12, LeafIndices: []uint32{0}, ParticipantAddress: "victim",
	})
	require.ErrorIs(t, err, ErrLocalRequestFailure)
}
