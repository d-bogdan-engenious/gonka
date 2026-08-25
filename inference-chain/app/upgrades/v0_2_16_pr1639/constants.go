package v0_2_16_pr1639

// UpgradeName MUST exactly match the on-chain governance proposal name.
// pr-1639 (PoC late/double submit fix) ships no state migration; this is a
// binary-swap-only upgrade that runs a no-op RunMigrations.
const UpgradeName = "v0.2.16-pr-1639"
