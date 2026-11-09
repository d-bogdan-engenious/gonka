import com.productscience.EpochStage
import com.productscience.data.AppState
import com.productscience.data.Decimal
import com.productscience.data.EpochParams
import com.productscience.data.EpochValidationParams
import com.productscience.data.InferenceParams
import com.productscience.data.InferenceState
import com.productscience.data.PocParams
import com.productscience.data.StakeValidatorStatus
import com.productscience.data.UpdateParams
import com.productscience.data.getParticipant
import com.productscience.data.spec
import com.productscience.LocalCluster
import com.productscience.LocalInferencePair
import com.productscience.inferenceConfig
import com.productscience.initCluster
import com.productscience.logSection
import org.assertj.core.api.Assertions.assertThat
import org.junit.jupiter.api.Test
import org.junit.jupiter.api.Timeout
import org.tinylog.kotlin.Logger
import java.util.concurrent.TimeUnit

/**
 * QA-109 / PR #2207 — governance param `epoch_validation_params.min_epoch_acceptance_weight`:
 * the minimum RAW claimed PoC weight (store-commit count x time-normalization, BEFORE dynamic
 * coefficients) for one participant+model to enter the NEXT epoch. Default 1 = the historical
 * `claimedWeight < 1` gate.
 *
 * EmptyEpochFallbackTests already exercises the floor at its DEFAULT value (setPocWeight(0) ->
 * claimedWeight 0 < 1 -> everyone dropped). This test exercises the NEW, governance-raised
 * behavior: participants that keep POSITIVE weight (and would pass the default floor of 1) are
 * excluded from the next epoch once governance raises the floor above their raw weight, the chain
 * falls back instead of halting, and reverting the floor re-admits them.
 *
 * Reuses the EmptyEpochFallback no-preservation config so that when every miner is excluded,
 * PreservedParticipants is empty and the seatbelt re-seats the live validators (making the
 * all-excluded outcome observable without a halt).
 */
@Timeout(value = 25, unit = TimeUnit.MINUTES)
class EpochAcceptanceWeightQaTests : TestermintTest() {

    private val noPreservationSpec = spec {
        this[AppState::inference] = spec<InferenceState> {
            this[InferenceState::params] = spec<InferenceParams> {
                this[InferenceParams::epochParams] = spec<EpochParams> {
                    this[EpochParams::pocSlotAllocation] = Decimal.fromDouble(0.001)
                }
                this[InferenceParams::pocParams] = spec<PocParams> {
                    this[PocParams::pocV2Enabled] = true
                }
            }
        }
    }

    private val config = inferenceConfig.copy(
        genesisSpec = inferenceConfig.genesisSpec?.merge(noPreservationSpec) ?: noPreservationSpec,
    )

    // Raised far above any achievable raw weight for 3 small mock validators, regardless of the
    // time-normalization factor. Keeps the participants' real (positive) weight below the floor.
    private val raisedFloor = 1_000_000_000_000L

    @Test
    fun `governance min_epoch_acceptance_weight excludes positive-weight participants then reverts`() {
        logSection("=== QA-109: min_epoch_acceptance_weight gates epoch admission ===")

        val (cluster, genesis) = initCluster(
            joinCount = 2,
            config = config,
            reboot = true,
            resetMlNodes = false,
        )
        cluster.allPairs.forEach { it.waitForMlNodesToLoad() }
        val allPairs = cluster.allPairs

        logSection("Phase 1: Healthy PoC at the default floor (1) -> all 3 admitted with positive raw weight")
        genesis.waitForStage(EpochStage.CLAIM_REWARDS, offset = 2)
        val before = genesis.api.getActiveParticipants()
        val beforeAddresses = before.activeParticipants.participants.map { it.index }.toSet()
        val beforeEpochId = before.activeParticipants.epochId
        Logger.info("Active before raise: epochId=$beforeEpochId addresses=$beforeAddresses")
        assertThat(beforeAddresses)
            .describedAs("all 3 validators are admitted at the default floor of 1")
            .hasSize(3)
        allPairs.forEach { pair ->
            val p = before.activeParticipants.getParticipant(pair)
            assertThat(p).describedAs("${pair.name} active before raise").isNotNull
            assertThat(p!!.weight)
                .describedAs("${pair.name} has POSITIVE raw weight (would pass the default floor of 1)")
                .isGreaterThan(1L)
        }
        // v0.2.17 backfills the floor to the historical default of 1.
        val backfilled = genesis.getParams().epochValidationParams?.minEpochAcceptanceWeight
        Logger.info("Backfilled min_epoch_acceptance_weight = $backfilled")
        assertThat(backfilled)
            .describedAs("v0.2.17 backfills min_epoch_acceptance_weight to the default of 1")
            .isEqualTo(1L)

        logSection("Phase 2: Governance raises the floor FAR above every participant's raw weight")
        setAcceptanceFloor(genesis, cluster, raisedFloor)
        val readBack = genesis.getParams().epochValidationParams?.minEpochAcceptanceWeight
        assertThat(readBack)
            .describedAs("governance proposal set the raised floor on-chain")
            .isEqualTo(raisedFloor)

        logSection("Phase 3: Run the next PoC cycle under the raised floor")
        genesis.waitForStage(EpochStage.START_OF_POC)
        genesis.waitForStage(EpochStage.END_OF_POC_VALIDATION, offset = 2)
        genesis.waitForStage(EpochStage.CLAIM_REWARDS, offset = 2)

        logSection("Phase 4: Every positive-weight miner is excluded by the gov floor; chain falls back (no halt)")
        val raised = genesis.api.getActiveParticipants()
        val raisedEpochId = raised.activeParticipants.epochId
        val raisedAddresses = raised.activeParticipants.participants.map { it.index }.toSet()
        Logger.info("After raise: epochId=$raisedEpochId addresses=$raisedAddresses excluded=${raised.excludedParticipants.map { it.address }}")
        assertThat(raisedEpochId)
            .describedAs("epoch still advances via the fallback seatbelt instead of halting")
            .isGreaterThan(beforeEpochId)
        // All miners excluded (positive weight < raised floor) => ComputeNewWeights returns [] =>
        // seatbelt re-seats the previous live validators (same addresses, still bonded).
        assertThat(raisedAddresses)
            .describedAs("seatbelt re-seats the previous validators after every miner is excluded")
            .isEqualTo(beforeAddresses)
        val raisedValidators = genesis.node.getValidators().validators
        assertThat(raisedValidators).describedAs("staking set unchanged, chain healthy").hasSize(3)
        allPairs.forEach { pair ->
            val pubKey = pair.node.getValidatorInfo().key
            val v = raisedValidators.find { it.consensusPubkey.value == pubKey }
            assertThat(v).describedAs("${pair.name} remains a staking validator under the raised floor").isNotNull
            assertThat(v!!.statusEnum)
                .describedAs("${pair.name} stays BONDED under the raised floor")
                .isEqualTo(StakeValidatorStatus.BONDED)
        }

        logSection("Phase 5: Revert the floor to 1 -> all miners are re-admitted (causal proof it was the floor)")
        setAcceptanceFloor(genesis, cluster, 1L)
        assertThat(genesis.getParams().epochValidationParams?.minEpochAcceptanceWeight)
            .describedAs("floor reverted to 1").isEqualTo(1L)
        genesis.waitForStage(EpochStage.START_OF_POC)
        genesis.waitForStage(EpochStage.CLAIM_REWARDS, offset = 2)
        val recovered = genesis.api.getActiveParticipants()
        val recoveredAddresses = recovered.activeParticipants.participants.map { it.index }.toSet()
        Logger.info("After revert: epochId=${recovered.activeParticipants.epochId} addresses=$recoveredAddresses")
        assertThat(recovered.activeParticipants.epochId)
            .describedAs("epoch advances again after revert").isGreaterThan(raisedEpochId)
        assertThat(recoveredAddresses)
            .describedAs("all miners re-admitted once the floor is back to 1")
            .isEqualTo(beforeAddresses)
        allPairs.forEach { pair ->
            val p = recovered.activeParticipants.getParticipant(pair)
            assertThat(p).describedAs("${pair.name} re-admitted after revert").isNotNull
            assertThat(p!!.weight)
                .describedAs("${pair.name} keeps positive weight after recovery")
                .isGreaterThan(0L)
        }
    }

    /**
     * QA-109 SHARP finding (live-reproduced on tn3, 2026-11-09): a floor set IN-BAND — above only
     * SOME validators' raw weight, not all — is NOT protected by the empty-epoch seatbelt (which
     * only fires when EVERY miner is excluded). The sub-floor validators are really dropped: they
     * UNBOND and the consensus set shrinks (here to the single surviving validator). Recovery is
     * GATED: the dropped validators come back only once the floor is back in range AND the surviving
     * validator re-admits their PoC across epochs. This is the partial-cascade the uniform-raise
     * test above cannot show (its seatbelt re-seats everyone).
     */
    @Test
    fun `in-band floor really unbonds sub-floor validators then gated recovery re-bonds them`() {
        logSection("=== QA-109: in-band floor -> partial valset collapse + gated recovery ===")

        val (cluster, genesis) = initCluster(
            joinCount = 2,
            config = config,
            reboot = true,
            resetMlNodes = false,
        )
        cluster.allPairs.forEach { it.waitForMlNodesToLoad() }
        val joins = cluster.joinPairs

        logSection("Phase 1: split the band (genesis far above floor, joins far below) -> all 3 admitted at floor 1")
        genesis.setPocWeight(100_000L)
        joins.forEach { it.setPocWeight(5L) }
        genesis.waitForStage(EpochStage.START_OF_POC)
        genesis.waitForStage(EpochStage.CLAIM_REWARDS, offset = 2)
        genesis.node.waitForNextBlock(3)
        val before = genesis.api.getActiveParticipants()
        val beforeEpochId = before.activeParticipants.epochId
        assertThat(before.activeParticipants.participants)
            .describedAs("all 3 admitted at the default floor of 1").hasSize(3)
        assertThat(genesis.node.getCometValidators().validators)
            .describedAs("3 validators in the consensus set").hasSize(3)

        logSection("Phase 2: raise floor INTO the band (above the joins' 5, below genesis' 100000)")
        setAcceptanceFloor(genesis, cluster, 1_000L)
        genesis.waitForStage(EpochStage.START_OF_POC)
        genesis.waitForStage(EpochStage.CLAIM_REWARDS, offset = 2)
        genesis.node.waitForNextBlock(3)

        logSection("Phase 3: the 2 joins are REALLY dropped (no seatbelt) -> UNBONDING, consensus set shrinks to 1; genesis survives; no halt")
        val collapsed = genesis.api.getActiveParticipants()
        assertThat(collapsed.activeParticipants.epochId)
            .describedAs("chain keeps advancing on the surviving validator (no halt)")
            .isGreaterThan(beforeEpochId)
        joins.forEach { j ->
            assertThat(collapsed.activeParticipants.getParticipant(j))
                .describedAs("${j.name} dropped from the active set by the in-band floor").isNull()
        }
        assertThat(collapsed.activeParticipants.getParticipant(genesis))
            .describedAs("genesis survives (weight above floor)").isNotNull
        val collapsedVals = genesis.node.getValidators().validators
        joins.forEach { j ->
            val v = collapsedVals.find { it.consensusPubkey.value == j.node.getValidatorInfo().key }
            assertThat(v).describedAs("${j.name} still has a staking record").isNotNull
            assertThat(v!!.statusEnum)
                .describedAs("${j.name} is UNBONDING after the in-band drop (not protected by the seatbelt)")
                .isEqualTo(StakeValidatorStatus.UNBONDING)
        }
        assertThat(genesis.node.getCometValidators().validators)
            .describedAs("consensus set collapsed to the single surviving validator").hasSize(1)

        logSection("Phase 4: restore weight + revert floor -> gated recovery re-bonds the joins across epochs")
        joins.forEach { it.setPocWeight(100_000L) }
        setAcceptanceFloor(genesis, cluster, 1L)
        var recoveredAddresses = emptySet<String>()
        for (attempt in 1..4) {
            genesis.waitForStage(EpochStage.START_OF_POC)
            genesis.waitForStage(EpochStage.CLAIM_REWARDS, offset = 2)
            genesis.node.waitForNextBlock(3)
            recoveredAddresses = genesis.api.getActiveParticipants()
                .activeParticipants.participants.map { it.index }.toSet()
            Logger.info("Recovery attempt $attempt: active=${recoveredAddresses.size} -> $recoveredAddresses")
            if (recoveredAddresses.size == 3) break
        }
        assertThat(recoveredAddresses)
            .describedAs("all 3 re-admitted after the floor is back in range (gated recovery)").hasSize(3)
        assertThat(genesis.node.getCometValidators().validators)
            .describedAs("consensus set recovered to 3").hasSize(3)
        joins.forEach { j ->
            val v = genesis.node.getValidators().validators
                .find { it.consensusPubkey.value == j.node.getValidatorInfo().key }
            assertThat(v!!.statusEnum)
                .describedAs("${j.name} is BONDED again after recovery")
                .isEqualTo(StakeValidatorStatus.BONDED)
        }
    }

    private fun setAcceptanceFloor(genesis: LocalInferencePair, cluster: LocalCluster, floor: Long) {
        val current = genesis.getParams()
        val evp = current.epochValidationParams ?: EpochValidationParams(256, 128, 1)
        val updated = current.copy(epochValidationParams = evp.copy(minEpochAcceptanceWeight = floor))
        genesis.runProposal(cluster, UpdateParams(params = updated))
    }
}
