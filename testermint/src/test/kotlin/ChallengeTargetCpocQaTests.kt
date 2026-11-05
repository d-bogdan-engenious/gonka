import com.productscience.data.ConfirmationPoCPhase
import com.productscience.data.StakeValidatorStatus
import com.productscience.data.getParticipant
import com.productscience.logSection
import org.assertj.core.api.Assertions.assertThat
import org.junit.jupiter.api.Test
import org.junit.jupiter.api.Timeout
import org.tinylog.kotlin.Logger
import java.util.concurrent.TimeUnit

/**
 * GON-665 — QA adversarial e2e for PR #2206 ("challenge poc should not send cpoc commits").
 *
 * Contract under test: a participant that is a challenge target in epoch N must not
 * be able to land an ordinary confirmation-PoC (CPoC) commit for epoch N. The chain
 * rejects such a commit (ErrPoCChallengeCPoCCommit, code 1206) and the off-chain
 * validators skip the target, so the target is excluded from CPoC scoring entirely
 * and must NOT be penalized by the CPoC for "not submitting" — its confirmation
 * outcome is decided by the challenge, not the CPoC.
 *
 * Adversarial twist: the target is given a CPoC mock weight (3) that, under a low
 * alpha (0.5), WOULD jail it if it were measured in the CPoC — exactly the setup
 * that jails a participant in ConfirmationPoCFailTests ("participant jailed for
 * ratio below alpha"). With #2206 the target is excluded, so it stays ACTIVE and
 * BONDED and has no accepted CPoC commit for the stage, while the honest nodes'
 * CPoC commits ARE accepted. This is the integration complement to the keeper unit
 * tests that isolate the 1206 gate itself.
 */
@Timeout(value = 25, unit = TimeUnit.MINUTES)
class ChallengeTargetCpocQaTests : TestermintTest() {
    @Test
    fun `same-epoch challenge target is excluded from CPoC commit and not penalized`() {
        logSection("=== TEST (GON-665): same-epoch challenge target barred from CPoC commit ===")
        val env = bootPoCChallengeCluster(
            expectedConfirmationsPerEpoch = 0, // start disabled; we enable a saturated cPoC after the challenge exists
            pocStageDuration = 5,
            alphaThreshold = 0.5,
        )
        val genesis = env.genesis
        val join1 = env.join1
        val join2 = env.join2
        val target = join1.node.getColdAddress()
        val genesisAddr = genesis.node.getColdAddress()
        val join2Addr = join2.node.getColdAddress()

        logSection("Honest regular-PoC weights before create")
        setAllPocV2Weights(listOf(genesis, join1, join2), listOf(10, 10, 10))

        logSection("Opening a challenge on join1 (the target) in the current epoch")
        createChallengeFirst(genesis, target)
        val created = waitForOpenChallenge(genesis, target)
        Logger.info("challenge start=${created.startHeight} finish=${created.finish} epoch=${created.epochIndex}")

        logSection("Giving the target a CPoC weight that WOULD jail it if measured (3 < alpha*10 = 5)")
        genesis.setPocWeight(10)
        join1.setPocWeight(3)
        join2.setPocWeight(10)

        logSection("Enabling a saturated cPoC in the SAME epoch as the challenge")
        enableConfirmationPoc(env.cluster, genesis, expectedConfirmationsPerEpoch = 1000)
        val event = waitForConfirmationPoCInEpoch(genesis, created.epochIndex)
        Logger.info("cPoC triggered height=${event.triggerHeight} epoch=${event.epochIndex}")

        waitForConfirmationPoCPhase(genesis, ConfirmationPoCPhase.CONFIRMATION_POC_GENERATION)
        waitForConfirmationPoCPhase(genesis, ConfirmationPoCPhase.CONFIRMATION_POC_VALIDATION)
        waitForConfirmationPoCCompletion(genesis)

        logSection("ASSERT: the challenge target has NO accepted CPoC store-commit; honest nodes do")
        val stageCommits = genesis.node.getAllPoCV2StoreCommitsForStage(event.triggerHeight).commits
        val committers = stageCommits.map { it.participantAddress }.toSet()
        Logger.info("CPoC stage committers=$committers (target=$target)")
        // The 1206 gate / off-chain skip: the challenge target never lands a CPoC commit.
        assertThat(committers).doesNotContain(target)
        // CPoC commits are still being accepted for non-challenged participants (not a dead stage).
        // At least one honest node commits (both may not, due to per-event preservation).
        assertThat(committers).containsAnyOf(genesisAddr, join2Addr)

        logSection("ASSERT: the target is NOT penalized by the CPoC (excluded, not jailed)")
        assertThat(genesis.node.getRawParticipants().getParticipant(join1)?.status).isEqualTo("ACTIVE")
        val join1Validator = genesis.node.getValidators().validators.find {
            it.consensusPubkey.value == join1.node.getValidatorInfo().key
        }
        assertThat(join1Validator).isNotNull
        assertThat(join1Validator!!.statusEnum).isEqualTo(StakeValidatorStatus.BONDED)

        logSection("ASSERT: the challenge itself is still live for the target after the cPoC")
        val after = waitForOpenChallenge(genesis, target)
        assertThat(after.isOpen()).isTrue()

        // Restore honest weights so epoch settlement is clean.
        genesis.setPocWeight(10)
        join1.setPocWeight(10)
        join2.setPocWeight(10)
        logSection("TEST PASSED (GON-665): challenge target excluded from CPoC commit + not penalized")
    }
}
