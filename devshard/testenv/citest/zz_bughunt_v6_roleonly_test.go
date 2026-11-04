//go:build testenvci

package citest

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"devshard/testenv/citest/harness"
	"devshard/testenv/config"
	"devshard/testenv/mockopenai"

	"github.com/stretchr/testify/require"
)

// BUG V6-A (full-stack): a host whose upstream emits ONE role-only SSE delta (no content) then
// goes silent sets the gateway's first-token clock (hasFirstToken) while contentChunks stays 0.
// attemptDeadline / liveReceiptHolder (cmd/devshardctl/redundancy.go:3330 / :3218) then treat it
// as "streaming", so no escalation and no fail-closed fire, and the request hangs toward the
// ~30-min InferenceTimeout with healthy capacity idle. A correct gateway must terminate (serve
// or fail closed) well within a short bound. Raw compose logs are dumped as proof.
func TestBugHunt_V6_RoleOnlyFirstTokenStalls_FullStack(t *testing.T) {
	harness.SkipUnlessEnv(t, "TESTENV_CITEST")
	harness.RequireDocker(t)

	stack, cfg, eps := harness.BootAdversarialStack(t, "citest-bughunt-roleonly-*")
	ctl := harness.GatewayChatClient()
	t.Cleanup(func() {
		harness.ResetMockOpenAIFault(t, ctl, eps.MockOpenAIHTTP)
		harness.DumpComposeLogs(t, stack, "devshardctl", "mock-openai-0", "versiond-0", "versiond-1")
	})
	harness.WaitStackHealthy(t, stack, eps)
	harness.WaitGatewayChatReady(t, ctl, eps.GatewayHTTP, 3*time.Minute, stack)

	// Every executor upstream emits a role-only first delta and then hangs.
	on := true
	harness.PatchMockOpenAIFault(t, ctl, eps.MockOpenAIHTTP, mockopenai.FaultPatch{RoleOnlyThenHang: &on})

	bodyJSON, err := json.Marshal(map[string]any{
		"model":      config.PrimaryModelID(cfg),
		"stream":     true,
		"max_tokens": 32,
		"messages":   []map[string]string{{"role": "user", "content": "citest v6 role-only first-token stall"}},
	})
	require.NoError(t, err)

	const bound = 120 * time.Second
	client := &http.Client{Timeout: bound + 90*time.Second} // generous; the bug hangs far past `bound`
	httpReq, err := http.NewRequest(http.MethodPost, eps.GatewayHTTP+"/v1/chat/completions", bytes.NewReader(bodyJSON))
	require.NoError(t, err)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+harness.TestenvAdminAPIKey)

	harness.Step(t, "driving one streaming chat; every host emits role-only then hangs")
	start := time.Now()
	resp, derr := client.Do(httpReq)
	if derr == nil {
		_, _ = io.ReadAll(resp.Body)
		_ = resp.Body.Close()
	}
	elapsed := time.Since(start)
	t.Logf("gateway stream terminated after %s (err=%v)", elapsed, derr)

	require.Less(t, elapsed, bound,
		"BUG V6-A: on a role-only-then-silent host the gateway neither escalated nor failed closed — "+
			"the request ran %s (>= %s), stalling toward the ~30-min InferenceTimeout; a correct gateway "+
			"terminates in seconds (see dumped devshardctl logs for send_failed/picker/no-escalation)", elapsed, bound)
}
