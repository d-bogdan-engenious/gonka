import com.productscience.EpochStage
import com.productscience.data.*
import com.productscience.initCluster
import com.productscience.logSection
import org.assertj.core.api.Assertions.assertThat
import org.junit.jupiter.api.Test
import org.junit.jupiter.api.Timeout
import org.tinylog.kotlin.Logger
import java.time.Duration
import java.util.concurrent.TimeUnit

/**
 * GON-593 — QA adversarial e2e: the long-running PoC (PoCChallenge) must survive a
 * chain software upgrade.
 *
 * An OPEN challenge (with committed segment data) is created mid-inference, then a
 * gov software-upgrade is applied via cosmovisor at a height that falls while the
 * challenge is still OPEN. We then prove the challenge record survived the binary
 * swap by querying its state AT the post-upgrade height (historical query — robust
 * even if the challenge later settles at the epoch boundary), and that the chain
 * did not halt at the upgrade height.
 *
 * The v0.2.17 upgrade handler runs no migration over challenge state and challenge
 * records live only in IAVL (not genesis), so an in-place cosmovisor swap should
 * preserve them. This exercises that continuity on a real cluster. (The definitive
 * check is the mainline v0.2.16->v0.2.17 upgrade on tn3; this is the harness edge.)
 *
 * We "upgrade" to the same local build (version-bumped) via getLocalUpgradeBinaries-
 * style staging under ../public-html/v2, which still drives the real cosmovisor
 * swap + restart + state continuity that the survival claim depends on.
 *
 * A longer epoch (120) is used so the inference phase outlives the gov voting
 * period (30s) plus the upgrade lead, keeping the challenge OPEN across the height.
 */
@Timeout(value = 60, unit = TimeUnit.MINUTES)
class LongRunningPocUpgradeQaTests : TestermintTest() {
    @Test
    fun `open PoC challenge survives a cosmovisor software upgrade`() {
        logSection("=== TEST (GON-593): long-running PoC survives an upgrade ===")
        val env = bootLongEpochChallengeCluster()
        val cluster = env.cluster
        val genesis = env.genesis
        val join1 = env.join1
        val target = join1.node.getColdAddress()
        // The upgrade swaps node+api binaries; tell the harness to rebuild the cluster afterwards.
        genesis.markNeedsReboot()

        logSection("Honest regular-PoC weights before create")
        setAllPocV2Weights(listOf(genesis, env.join1, env.join2), listOf(10, 10, 10))

        logSection("Opening a challenge on join1 (the target) in the current inference phase")
        createChallengeFirst(genesis, target)
        val before = waitForOpenChallenge(genesis, target)
        waitForChallengeCommit(genesis, target) // ensure real committed segment state exists in IAVL
        Logger.info("challenge start=${before.startHeight} finish=${before.finish} epoch=${before.epochIndex}")

        logSection("Scheduling a cosmovisor upgrade while the challenge is still OPEN")
        val now = genesis.getCurrentBlockHeight()
        val upgradeHeight = now + 40 // lead for the 30s gov voting period + margin
        require(upgradeHeight < before.finish - 2) {
            "upgrade height $upgradeHeight is not comfortably before challenge finish ${before.finish}"
        }
        val binaries = stagedUpgradeBinaries("v2/inferenced", "inferenced", genesis)
        val apiBinaries = stagedUpgradeBinaries("v2/dapi", "decentralized-api", genesis)

        // Must be the upgrade name the test build (go:build upgraded) registers a handler for
        // (v2test.UpgradeName). An unknown name makes the chain panic at the upgrade height.
        val resp = genesis.submitUpgradeProposal(
            title = "v0.0.1test",
            description = "GON-593: long-running PoC across upgrade",
            binaries = binaries,
            apiBinaries = apiBinaries,
            height = upgradeHeight,
            nodeVersion = "",
            deposit = 1000000,
        )
        require(resp.code == 0) { "upgrade proposal failed: ${resp.rawLog}" }
        val proposalId = requireNotNull(resp.getProposalId()) { "no proposal id" }
        val govParams = genesis.node.getGovParams().params
        genesis.makeGovernanceDeposit(proposalId, govParams.minDeposit.first().amount)
        logSection("Voting on the upgrade proposal")
        cluster.allPairs.forEach { pair ->
            val vote = pair.voteOnProposal(proposalId, "yes")
            require(vote.code == 0) { "vote failed for ${pair.name}: ${vote.rawLog}" }
        }
        Thread.sleep(govParams.votingPeriod.plus(Duration.ofSeconds(5)).toMillis())

        logSection("Waiting for the upgrade to apply at height $upgradeHeight")
        genesis.node.waitForMinimumBlock(upgradeHeight - 2, "pre-upgrade height")
        // The node container stops and cosmovisor relaunches it at the upgrade height; CLI execs
        // against it throw (409 "container not running" / socket) during that window. Poll tolerantly
        // rather than calling waitForNextBlock (which would crash on the down container).
        require(waitForUpgradeApplied(genesis, upgradeHeight, Duration.ofMinutes(10))) {
            "node did not come back with upgrade $upgradeHeight applied (chain halted or handler missing)"
        }

        logSection("ASSERT: cosmovisor applied the upgrade and the chain did not halt")
        val resumedHeight = retryQuery { genesis.getCurrentBlockHeight() }
        assertThat(resumedHeight).isGreaterThan(upgradeHeight)
        Logger.info("chain resumed past upgrade: height=$resumedHeight (upgrade=$upgradeHeight)")

        logSection("ASSERT: the OPEN challenge survived the binary swap (queried at the post-upgrade height)")
        val surviveHeight = upgradeHeight + 2
        retryQuery { genesis.node.waitForMinimumBlock(surviveHeight, "post-upgrade survive height") }
        val after = retryQuery { challengeOf(genesis, target, height = surviveHeight) }
            ?: error("challenge for $target was LOST across the upgrade (absent at height $surviveHeight)")
        assertThat(after.target).isEqualTo(target)
        assertThat(after.epochIndex).isEqualTo(before.epochIndex)
        assertThat(after.isOpen()).isTrue()
        assertThat(after.commits).isNotEmpty() // committed segment data preserved too
        Logger.info("challenge survived: start=${after.startHeight} finish=${after.finish} state=${after.state} commits=${after.commits.size}")

        logSection("TEST PASSED (GON-593): OPEN PoC challenge + segment data survived the cosmovisor upgrade")
    }

    // ------------------------------------------------------------------
    // helpers
    // ------------------------------------------------------------------

    /** Long-epoch variant of bootPoCChallengeCluster so the inference phase outlives gov voting + upgrade lead. */
    private fun bootLongEpochChallengeCluster(): PoCChallengeCluster {
        val spec = spec {
            this[AppState::inference] = spec<InferenceState> {
                this[InferenceState::params] = spec<InferenceParams> {
                    this[InferenceParams::epochParams] = spec<EpochParams> {
                        this[EpochParams::epochLength] = 120L
                        this[EpochParams::pocStageDuration] = 5L
                        this[EpochParams::pocValidationDuration] = 4L
                        this[EpochParams::pocExchangeDuration] = 2L
                        this[EpochParams::pocValidationDelay] = 1L
                        this[EpochParams::setNewValidatorsDelay] = 1L
                        this[EpochParams::confirmationPocSafetyWindow] = 0L
                    }
                    this[InferenceParams::confirmationPocParams] = spec<ConfirmationPoCParams> {
                        this[ConfirmationPoCParams::expectedConfirmationsPerEpoch] = 0L
                        this[ConfirmationPoCParams::alphaThreshold] = Decimal.fromDouble(0.70)
                        this[ConfirmationPoCParams::slashFraction] = Decimal.fromDouble(0.10)
                    }
                    this[InferenceParams::pocParams] = spec<PocParams> {
                        this[PocParams::pocDataPruningEpochThreshold] = 10L
                        this[PocParams::pocV2Enabled] = true
                    }
                    this[InferenceParams::pocChallengeParams] = spec<PocChallengeParams> {
                        this[PocChallengeParams::minPunishableSegmentBlocks] = 8L
                        this[PocChallengeParams::maxActiveChallenges] = 4L
                        this[PocChallengeParams::paymentRatio] = Decimal.fromDouble(0.1)
                    }
                }
            }
        }
        val (cluster, genesis) = initCluster(
            joinCount = 2,
            mergeSpec = spec,
            reboot = true,
            resetMlNodes = false,
        )
        val join1 = cluster.joinPairs[0]
        val join2 = cluster.joinPairs[1]

        logSection("Waiting for epoch 1 inference")
        genesis.waitForStage(EpochStage.START_OF_POC)
        genesis.waitForStage(EpochStage.CLAIM_REWARDS)
        waitUntilInference(genesis)

        logSection("Allowing genesis as challenger during epoch 1")
        allowChallenger(cluster, genesis)

        logSection("Waiting for epoch 2 inference")
        genesis.waitForStage(EpochStage.START_OF_POC)
        genesis.waitForStage(EpochStage.CLAIM_REWARDS)
        waitUntilInference(genesis)

        return PoCChallengeCluster(cluster, genesis, join1, join2)
    }

    /** Build the upgrade-info binaries map from the locally-built archives under ../public-html/v2 (amd64). */
    private fun stagedUpgradeBinaries(baseDir: String, archiveBaseName: String, genesis: com.productscience.LocalInferencePair): Map<String, String> {
        val rel = "$baseDir/$archiveBaseName-amd64.zip"
        val local = "../public-html/$rel"
        val sha = getSha256Checksum(local)
        return mapOf("linux/amd64" to "http://genesis-mock-server:8080/files/$rel?checksum=sha256:$sha")
    }

    /**
     * Polls getLastUpgradeHeight tolerantly (the node container is down during the cosmovisor
     * swap, so CLI execs throw) until the upgrade is applied or the deadline passes.
     */
    private fun waitForUpgradeApplied(
        genesis: com.productscience.LocalInferencePair,
        upgradeHeight: Long,
        timeout: Duration,
    ): Boolean {
        val deadline = System.currentTimeMillis() + timeout.toMillis()
        while (System.currentTimeMillis() < deadline) {
            val applied = runCatching {
                val resp = genesis.node.getLastUpgradeHeight()
                resp.found && resp.lastUpgradeHeight == upgradeHeight
            }.getOrDefault(false)
            if (applied) return true
            runCatching { Thread.sleep(3000) }
        }
        return false
    }

    /** Retry a query that may transiently fail while the node container is restarting. */
    private fun <T> retryQuery(attempts: Int = 30, block: () -> T): T {
        var last: Throwable? = null
        repeat(attempts) {
            val r = runCatching(block)
            if (r.isSuccess) return r.getOrThrow()
            last = r.exceptionOrNull()
            runCatching { Thread.sleep(2000) }
        }
        throw last ?: IllegalStateException("retryQuery exhausted")
    }
}
