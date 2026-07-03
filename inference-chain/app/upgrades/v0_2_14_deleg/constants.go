package v0_2_14_deleg

// UpgradeName is a throwaway, testnet-only upgrade name used to gov-swap the
// running testnet-3 binary to the delegation consensus-weight vulnerability fix
// (PR #1378) layered on top of the v0.2.14-claimrec2 lineage.
//
// PR #1378 makes PoC delegation reward-only: delegation no longer moves
// ActiveParticipant.Weight (consensus weight). Instead a per-epoch
// DelegationRewardTransferSnapshot (new key prefix 108) records the reward
// transfers/penalties and settlement applies them to the reward-weight map
// only. This handler mirrors v0.2.14-claimrec2 but ALSO seeds an empty
// snapshot for the current effective epoch so the first post-upgrade
// settlement has a well-defined (empty) snapshot to read.
const UpgradeName = "v0.2.14-deleg"
