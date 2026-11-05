package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// BUG V6-I (recovery / funds-stranding, narrow): commitmentRoutePrefix (escrow_rotator.go:602)
// rebuilds a pending commitment's birth route prefix as VersionedRoutePrefix("v" + committed),
// where `committed` is the version STAMP (commitment.ProtocolVersion). The leading "v" is only
// correct for numeric versions, whose stamp drops the v (ParseProtocolVersion: "/devshard/v5" ->
// stamp "5" -> "v"+"5" = "v5"). A NAMED runtime is stamped AS-IS (ParseProtocolVersion leaves
// "mainnet-canary" unchanged), so "v"+"mainnet-canary" = "vmainnet-canary" -> /devshard/vmainnet-
// canary, a prefix no host serves. On recovery after the gateway moved to another version, the
// escrow is re-pinned to that bogus prefix and never binds (ErrSessionVersionConflict forever),
// stranding its funds. Distinct from the cosmetic-log #1542 (which does not fail rotation).
func TestBugHunt_V6_CommitmentRoutePrefixNamedRuntimeStrands(t *testing.T) {
	// The gateway now serves a numeric version; the pending commitment was minted under a NAMED
	// runtime, so its stamp != the gateway's current version and recovery takes the reconstruct path.
	t.Setenv("DEVSHARD_ROUTE_PREFIX", "/devshard/v6")

	got := commitmentRoutePrefix(GatewayEscrowCommitment{ProtocolVersion: "mainnet-canary"})

	assert.Equal(t, "/devshard/mainnet-canary", got,
		"BUG V6-I: a named-runtime commitment must recover to its birth prefix /devshard/mainnet-canary, "+
			"but commitmentRoutePrefix returned %q — a spurious leading 'v' from \"v\"+committed, a prefix no "+
			"host serves, so the recovered escrow never binds (ErrSessionVersionConflict) and its funds are stranded",
		got)
}
