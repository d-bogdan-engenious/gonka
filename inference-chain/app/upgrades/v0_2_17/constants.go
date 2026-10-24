package v0_2_17

// UpgradeName MUST exactly match the on-chain governance proposal name.
// Renamed from "v0.2.17" to a unique name so this handler can be applied on a
// chain whose running binary already registers "v0.2.17" (gonka testnet-3 after
// the decode-simplify upgrade) without tripping cosmovisor's pre-trigger guard.
const UpgradeName = "v0.2.17-safeguards-qa93"
