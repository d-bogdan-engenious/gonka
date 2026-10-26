package v0_2_17

// UpgradeName MUST exactly match the on-chain governance proposal name.
// Renamed from "v0.2.17" to a unique name so this handler can be applied on a
// chain whose running binary already registers "v0.2.17" / "v0.2.17-safeguards-qa93"
// (gonka testnet-3) without tripping cosmovisor's pre-trigger guard. This build
// adds PR #2021 (feegrant refresh), #2114 (bridge wallet-address query) and
// #1995 (dapi PoC-proof read bound) on top of the v0.2.17 safeguards set.
const UpgradeName = "v0.2.17-qa98100"
