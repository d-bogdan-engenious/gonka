package v0_2_14_qa40b

// UpgradeName is a throwaway, testnet-only upgrade name used by QA-40 to
// exercise a real cosmovisor upgrade (chain halt -> binary swap -> resume) and
// observe devshard/gateway/all-container behaviour during the upgrade window.
// The handler is an intentional no-op layered on the v0.2.14-claimrec2 lineage:
// same binary/state, no new prefixes, no migrations to backfill.
const UpgradeName = "v0.2.14-qa40b"
