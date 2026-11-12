package mlnodeclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

// Coverage for the PoC v2 HTTP builder methods (previously 0%): success path (200 + JSON
// decode) and the status>=400 error path.
func TestPoCV2Requests_SuccessAndErrorStatus(t *testing.T) {
	ctx := context.Background()

	okJSON := func(body string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(body))
		}))
	}
	errSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("boom"))
	}))
	defer errSrv.Close()

	t.Run("InitGenerateV2", func(t *testing.T) {
		s := okJSON(`{"status":"ok"}`)
		defer s.Close()
		resp, err := NewNodeClient(s.URL, s.URL).InitGenerateV2(ctx, PoCInitGenerateRequestV2{})
		require.NoError(t, err)
		require.NotNil(t, resp)
		_, err = NewNodeClient(errSrv.URL, errSrv.URL).InitGenerateV2(ctx, PoCInitGenerateRequestV2{})
		require.Error(t, err)
	})

	t.Run("GenerateV2", func(t *testing.T) {
		s := okJSON(`{"status":"ok"}`)
		defer s.Close()
		resp, err := NewNodeClient(s.URL, s.URL).GenerateV2(ctx, PoCGenerateRequestV2{})
		require.NoError(t, err)
		require.NotNil(t, resp)
		_, err = NewNodeClient(errSrv.URL, errSrv.URL).GenerateV2(ctx, PoCGenerateRequestV2{})
		require.Error(t, err)
	})

	t.Run("GetPowStatusV2", func(t *testing.T) {
		s := okJSON(`{"status":"IDLE"}`)
		defer s.Close()
		resp, err := NewNodeClient(s.URL, s.URL).GetPowStatusV2(ctx)
		require.NoError(t, err)
		require.Equal(t, "IDLE", resp.Status)
		_, err = NewNodeClient(errSrv.URL, errSrv.URL).GetPowStatusV2(ctx)
		require.Error(t, err)
	})

	t.Run("StopPowV2", func(t *testing.T) {
		s := okJSON(`{"status":"ok"}`)
		defer s.Close()
		resp, err := NewNodeClient(s.URL, s.URL).StopPowV2(ctx)
		require.NoError(t, err)
		require.NotNil(t, resp)
		_, err = NewNodeClient(errSrv.URL, errSrv.URL).StopPowV2(ctx)
		require.Error(t, err)
	})
}
