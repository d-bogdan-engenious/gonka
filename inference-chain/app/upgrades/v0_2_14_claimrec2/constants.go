package v0_2_14_claimrec2

// UpgradeName is a throwaway, testnet-only upgrade name used to gov-swap the
// running testnet-3 binary to the claim-recipient build that additionally
// carries PR #1377 ("Respect reward recipient for stale devshard escrows").
// It layers the devshard claim-recipient routing fix on top of the
// v0.2.14-claimrec lineage.
const UpgradeName = "v0.2.14-claimrec2"
