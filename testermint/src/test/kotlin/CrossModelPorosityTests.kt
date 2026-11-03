import com.productscience.*
import com.productscience.data.*
import org.assertj.core.api.Assertions.assertThat
import org.junit.jupiter.api.Test

/**
 * QA-107 / PR #2196 ("prevent cross-model nonce gaps from failing porosity").
 *
 * The DAPI assigns PoC nonce node numbering, and the chain checks porosity
 * (maxNonce/count, fails at >=100) PER MODEL. Before #2196 the node_count was the
 * participant's GLOBAL node total, so a model served by few nodes among many got a
 * sparse (high-stride) numbering and could fail porosity on valid work.
 *
 * #2196 numbers nodes PER MODEL: a model with N nodes reports node_count=N, and a
 * lone-node model reports node_count=1 (dense), independent of other models' nodes.
 *
 * This test drives an imbalanced per-model topology on one participant and asserts,
 * via the mock's record of the node_count the DAPI sent, that numbering is per-model
 * (no node sees the global total). Without #2196 every node would report the global
 * total, failing this test.
 */
class CrossModelPorosityTests : TestermintTest() {

    private val multiModelSpec = spec {
        this[AppState::inference] = spec<InferenceState> {
            this[InferenceState::params] = spec<InferenceParams> {
                this[InferenceParams::pocParams] = spec<PocParams> {
                    this[PocParams::models] = listOf(
                        PoCModelConfig(
                            modelId = defaultModel,
                            schemes = listOf(
                                PocSchemeParams(
                                    seqLen = 256L,
                                    dynamicCoefficient = DynamicCoefficientModelConfig(
                                        coeffMin = Decimal.fromDouble(1.0),
                                        coeffMax = Decimal.fromDouble(1.0),
                                        relativeDifficulty = Decimal.fromDouble(1.0),
                                        targetShareBps = 5000,
                                    ),
                                ),
                            ),
                        ),
                        PoCModelConfig(
                            modelId = secondModel,
                            schemes = listOf(
                                PocSchemeParams(
                                    seqLen = 256L,
                                    dynamicCoefficient = DynamicCoefficientModelConfig(
                                        coeffMin = Decimal.fromDouble(1.0),
                                        coeffMax = Decimal.fromDouble(1.0),
                                        relativeDifficulty = Decimal.fromDouble(1.0),
                                        targetShareBps = 5000,
                                    ),
                                ),
                            ),
                        ),
                    )
                    this[PocParams::pocV2Enabled] = true
                    this[PocParams::validationSlots] = 2L
                    this[PocParams::pocNormalizationEnabled] = false
                }
            }
            // Disable power capping so the imbalanced topology can't trip it.
            this[InferenceState::genesisOnlyParams] = spec<GenesisOnlyParams> {
                this[GenesisOnlyParams::maxIndividualPowerPercentage] = Decimal.fromDouble(0.0)
            }
        }
    }

    @Test
    fun `cross-model node numbering is per-model not global`() {
        val (cluster, genesis) = initCluster(2, reboot = true, mergeSpec = multiModelSpec)

        // Let the initial (default single-node) epoch finish before reconfiguring.
        genesis.waitForStage(EpochStage.SET_NEW_VALIDATORS)

        logSection("Imbalanced topology on genesis: 3 MLNodes on model_a + 1 on model_b")
        val pairName = genesis.name.trim('/')
        val modelANodes = (1..3).map { i ->
            validNode.copy(
                id = "node-a-$i",
                host = "ml-000$i.$pairName.test",
                models = mapOf(defaultModel to ModelConfig(args = emptyList())),
            )
        }
        val modelBNode = validNode.copy(
            id = "node-b-1",
            host = "ml-0004.$pairName.test",
            models = mapOf(secondModel to ModelConfig(args = emptyList())),
        )
        val allNodes = modelANodes + modelBNode
        val totalNodes = allNodes.size // 4 (the pre-#2196 global node_count)

        genesis.api.setNodesTo(allNodes)
        allNodes.forEach { genesis.mock?.setPocResponse(10L, it.pocHost) }

        logSection("Run PoC cycles with the imbalanced topology")
        genesis.waitForStage(EpochStage.SET_NEW_VALIDATORS, 3)

        val observed = genesis.mock!!.getPocNodeCounts()
        logSection("Observed PoC node_counts on genesis mock: $observed (global total would be $totalNodes)")

        // With #2196: model_a nodes report per-model count=3, model_b reports 1,
        // and NO node reports the global total (4).
        // Without #2196: every node reports the global total (4).
        assertThat(observed)
            .describedAs("no node should receive the global node total ($totalNodes) — numbering must be per-model")
            .doesNotContain(totalNodes)
        assertThat(observed)
            .describedAs("model_a group has ${modelANodes.size} nodes -> its per-model node_count must be observed")
            .contains(modelANodes.size)
    }
}
