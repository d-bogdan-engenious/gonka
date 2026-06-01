package v0_2_13

const UpgradeName = "v0.2.13"

// DevshardQAUpgradeName is a throwaway upgrade name used ONLY for QA on a chain
// that has already applied the real "v0.2.13" upgrade (e.g. testnet-3). It does
// not collide with any real version name, so future official upgrades remain
// schedulable. See CreateDevshardQAUpgradeHandler.
const DevshardQAUpgradeName = "v0_2_13_devshard2_qa"
