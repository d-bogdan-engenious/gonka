package mlnodeclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/productscience/inference/x/inference/types"
	"github.com/stretchr/testify/require"
)

// TestPoCCallbackBase_SchemeRouting pins the DECODE-vs-PREFILL callback suffix split.
func TestPoCCallbackBase_SchemeRouting(t *testing.T) {
	require.Equal(t, "http://api/v3/poc-batches",
		PoCCallbackBase("http://api", types.PocScheme_POC_SCHEME_DECODE))
	require.Equal(t, "http://api/v2/poc-batches",
		PoCCallbackBase("http://api", types.PocScheme_POC_SCHEME_PREFILL))
	// Any non-DECODE scheme (incl. the zero/unspecified value) takes the v2 path.
	require.Equal(t, "http://api/v2/poc-batches",
		PoCCallbackBase("http://api", types.PocScheme(0)))
}

// TestPoCV2Requests_UnmarshalErrorOn200 covers the json.Unmarshal error branch:
// a 200 response whose body is not valid JSON must surface an error, not a value.
func TestPoCV2Requests_UnmarshalErrorOn200(t *testing.T) {
	ctx := context.Background()
	badJSON := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("this is not json"))
	}))
	defer badJSON.Close()

	c := NewNodeClient(badJSON.URL, badJSON.URL)

	_, err := c.InitGenerateV2(ctx, PoCInitGenerateRequestV2{})
	require.Error(t, err)
	_, err = c.GenerateV2(ctx, PoCGenerateRequestV2{})
	require.Error(t, err)
	_, err = c.GetPowStatusV2(ctx)
	require.Error(t, err)
	_, err = c.StopPowV2(ctx)
	require.Error(t, err)
}

// TestPoCV2Requests_TransportError covers the request-send (SendPost/SendGet) error
// branch: pointing the client at a closed listener yields a connection-refused error
// before any status/body handling runs.
func TestPoCV2Requests_TransportError(t *testing.T) {
	ctx := context.Background()
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	dead := srv.URL
	srv.Close() // nothing listens on dead anymore

	c := NewNodeClient(dead, dead)

	_, err := c.InitGenerateV2(ctx, PoCInitGenerateRequestV2{})
	require.Error(t, err)
	_, err = c.GenerateV2(ctx, PoCGenerateRequestV2{})
	require.Error(t, err)
	_, err = c.GetPowStatusV2(ctx)
	require.Error(t, err)
	_, err = c.StopPowV2(ctx)
	require.Error(t, err)
}
