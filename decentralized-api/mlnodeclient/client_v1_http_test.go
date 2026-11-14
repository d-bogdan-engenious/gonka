package mlnodeclient

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

// Covers the V1 / node-management HTTP client methods (NodeState, GetPowStatus,
// InferenceHealth, InferenceUp, GetLoadedModels) on both the success and the
// non-200 error path, via an httptest stub. These are the dapi→mlnode control
// calls used around PoC (stop generation, poll status, node health).
func TestClientV1HTTPMethods_SuccessAndError(t *testing.T) {
	ctx := context.Background()
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"status":"IDLE","is_model_initialized":true,"data":[{"id":"m"}]}`)
	}))
	defer ok.Close()
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer bad.Close()

	okc := NewNodeClient(ok.URL, ok.URL)
	badc := NewNodeClient(bad.URL, bad.URL)

	t.Run("NodeState", func(t *testing.T) {
		_, err := okc.NodeState(ctx)
		require.NoError(t, err)
		_, err = badc.NodeState(ctx)
		require.Error(t, err)
	})
	t.Run("GetPowStatus", func(t *testing.T) {
		_, err := okc.GetPowStatus(ctx)
		require.NoError(t, err)
		_, err = badc.GetPowStatus(ctx)
		require.Error(t, err)
	})
	t.Run("InferenceHealth", func(t *testing.T) {
		up, err := okc.InferenceHealth(ctx)
		require.NoError(t, err)
		require.True(t, up)
		_, err = badc.InferenceHealth(ctx)
		require.Error(t, err)
	})
	t.Run("InferenceUp", func(t *testing.T) {
		require.NoError(t, okc.InferenceUp(ctx, "model-a", nil))
		require.Error(t, badc.InferenceUp(ctx, "model-a", nil))
	})
	t.Run("GetLoadedModels", func(t *testing.T) {
		models, err := okc.GetLoadedModels(ctx)
		require.NoError(t, err)
		require.Equal(t, []string{"m"}, models)
		_, err = badc.GetLoadedModels(ctx)
		require.Error(t, err)
	})
}
