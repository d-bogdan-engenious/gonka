package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// BUG V6-I (recovery / funds-stranding): commitmentRoutePrefix (escrow_rotator.go:602) rebuilds a
// pending commitment's birth route prefix as VersionedRoutePrefix("v" + committed), where
// `committed` is the gateway-DB version STAMP = escrowProtocolVersionFor(birthPrefix) =
// string(ParseProtocolVersion(segment)). ParseProtocolVersion is LOSSY for any non-plain version:
//   - plain "v6" -> "6"      -> "v"+"6"   = v6        (round-trips, safe)
//   - single minor "v4.2" -> "4.2" -> v4.2            (round-trips, safe)
//   - patch "v6.0.1"  -> "6.0" (patch dropped)        -> v6.0        != v6.0.1
//   - multi-minor "v2.1.0" -> "2.1"                   -> v2.1        != v2.1.0
//   - suffix "v4.1r5" -> "4.1" (suffix dropped)       -> v4.1        != v4.1r5
//   - named "mainnet-canary" -> as-is                 -> vmainnet-canary (spurious leading v)
// So on recovery after the gateway moved versions, an escrow born on any non-plain route prefix is
// re-pinned to a prefix NO host serves and never binds -> its funds strand. The release vocabulary
// is dotted/suffixed (release tags devshard/v6.0.0-rc1, v4.1.0), so this is a realistic footgun, not
// just a named-runtime edge. (Distinct from the cosmetic-log #1542.)
//
// Driven through the real creation path (escrowProtocolVersionFor computes the stored stamp) and the
// real recovery path (commitmentRoutePrefix). Recovery must rebuild the SAME prefix the escrow was
// born on; it does not for any non-plain version.
func TestBugHunt_V6_CommitmentRoutePrefixNamedRuntimeStrands(t *testing.T) {
	// Gateway has moved to a different version, so recovery takes the reconstruct branch.
	t.Setenv("DEVSHARD_ROUTE_PREFIX", "/devshard/v7")

	cases := []struct {
		birth string
		safe  bool
	}{
		{"/devshard/v6", true},               // plain vN - safe (control)
		{"/devshard/v4.2", true},             // single minor - safe (control)
		{"/devshard/v6.0.1", false},          // patch dropped -> /devshard/v6.0
		{"/devshard/v2.1.0", false},          // second minor dropped -> /devshard/v2.1
		{"/devshard/v4.1r5", false},          // suffix dropped -> /devshard/v4.1
		{"/devshard/mainnet-canary", false},  // named -> /devshard/vmainnet-canary
	}
	for _, c := range cases {
		stamp := escrowProtocolVersionFor(c.birth) // what gets stored at creation
		got := commitmentRoutePrefix(GatewayEscrowCommitment{ProtocolVersion: stamp})
		assert.Equalf(t, c.birth, got,
			"BUG V6-I: an escrow born on %q (stored stamp %q) recovers to %q — a non-plain version is "+
				"truncated/mangled by ParseProtocolVersion, so the recovered escrow pins to a prefix no host "+
				"serves and never binds (funds strand). Recovery must rebuild the birth prefix, not \"v\"+stamp.",
			c.birth, stamp, got)
	}
}
