# PoC / consensus regression coverage map

How we test gonka's PoC/consensus core: a **live tn3 regression suite** (automates the manual QA
scenarios, server-switchable, CI on trigger) backed by **Go unit/integration tests** for
deterministic logic. This is NOT a plan to duplicate dev unit tests — the live suite is the primary
regression vehicle; Go tests fill dev-uncovered branches and reproduce adversarial findings.

Two homes:
- Go tests + this map → `d-bogdan-engenious/gonka` branch `regression/poc-coverage-suite`.
- Live harness → private repo `d-bogdan-engenious/gonka-tn-regression` (`./run.sh --server tn3`).

Environment under test: tn3 (`gonka-testnet-3`), build `632338e` / app_version 7 (v0.2.17 line).

---

## A. Live tn3 regression — 11 scenarios / 48 checks (green locally + CI)

Each scenario automates a manual QA check and asserts one decision/branch per check. CI =
GitHub Actions `workflow_dispatch` (trigger-based), confirmed green end-to-end on a hosted runner.

| # | Feature tested | HOW (what it asserts, from live tn3 queries) |
|---|---|---|
| 01 | Chain liveness / validator set | `/chain-rpc/status` network==chain_id, not catching_up, height advances across a 6s wait; `/validators` count ≥1 |
| 02 | Epoch geometry (freeze-guards) | `/params` epoch_shift < epoch_length AND epoch_length > 94 (the freeze floors) |
| 03 | Epoch-transition outputs (`onSetNewValidatorsStage`/`computePrice`/`getCurrentValidatorWeights`) | `current_epoch_group_data`: group_id>0, unit_of_compute_price>0, total_weight>0, members with positive weight, no voting_power<0 |
| 04 | PoC weight-earning cycle | `all_poc_v2_store_commits/{stage}`: ≥1 commit, all count>0 & 32-byte SMST root; committers ∩ active-set ≥1 (PoC→weight); active_set == validator_count |
| 05 | Weight health / anti-collapse | members≥2 (not collapsed to one), min member weight>0, Σweights==total_weight, no single-member majority (2·max<total) |
| 06 | Preserved-nodes snapshot (`SamplePreservedForEpisode`) | `preserved_nodes_snapshot`: anchor present AND == current poc_start (freshness), no malformed populated entry. (Empty set is a valid phase — not failed.) |
| 07 | Dangling-model halt alarm (F2 early-warning) | every `poc_params.models` id ∈ `models_all` governance ids (0 dangling) |
| 08 | Weight anomaly vs history | current vs previous epoch per-member weight; no member →0 or >4x swing |
| 09 | Config drift (STATEFUL baseline) | `baseline/<server>_baseline.json` vs live: chain_id, epoch_length/shift, poc_models, validator_count, unit_of_compute_price, sub_group_models — any change fails (re-bless by committing baseline) |
| 10 | Participant/validator status (fraud/slashing) | `/participant`: none INVALID, no consecutive_invalid_inferences; `excluded_participants/{epoch}` == 0 |
| 11 | Settlement / reward pipeline (`SettleAccounts`) | `epoch_performance_summary/{last}`: ≥1 summary, total rewarded>0, every participant rewarded (no silent zero), 0 invalidated, 0 unexpected burn |

Server-switchable: a target is `servers/<name>.env` (public RPC/API + chain-id, no secrets) +
`baseline/<name>_baseline.json`. Switch = `--server <name>`.

## B. Go unit/integration coverage (deterministic logic; dev-uncovered branches)

| Feature / function | How tested | Coverage |
|---|---|---|
| Dynamic coefficient (`coefficients/`) | branch-table over Freeze/adjust/clamp/normalize | **97.8%** |
| Validation gate (`pocValidated`, `guardianProtection`, `calculateParticipantWeight`, `calculateValidationOutcome`, `calculateTotalWeight`, thresholds, `ComputeNewWeights`) | grind tests | **100%** |
| Unit-of-compute price (`weightedMedian`/`ComputeUnitOfComputePrice`/`getWeight`) | branch-table: empty/zero-weight/even-odd/skew/default-fallback/nil/bad-string | **94–100%** |
| CPoC lifecycle (`checkConfirmationPoCTrigger`/`handleConfirmationPoC`/phase-transitions) | characterization over all early-return + trigger branches | **51–80%** |
| `getCurrentValidatorWeights` | reads current group member weights | covered |
| Store-commit → Count (`buildPoCV2CommitUpdate`/`persist`) | commit validation branches | **86–96%** |
| Pruning (class-6) | pruner coverage | **79–100%** (F1: RandomSeeds pruner MISSING — RED test) |
| Off-chain PoC validator (`checkValidateeProofs`/`validateLeafCoverage`/`sampleLeafIndices`/`isPorosityTooHigh`) | grind | **100% core** |
| F1 — RandomSeeds unpruned (class-6) | `random_seed_pruning_test.go` asserts pruned | **RED** (bug, reproduced) |
| F2 — dangling-model → epoch-formation halt (class-1) | keeper RED (missing guard) + module GREEN trigger + EndBlock integration repro | reproduced (NON-FINDING: gov-gated, mainnet #78/#87 scrub atomically) |

## C. Known findings posture

- **F1** (LOW): RandomSeeds not pruned → unbounded IAVL growth. Reproduced (RED). Only novel bug.
- **F2** NON-FINDING: dangling-model halt — gov-gated, mainnet does it atomically. Kept as live alarm (scenario 07).
- Refuted / by-design: Count-inflation (SMST sum-tree), determinism (clean), double-claim, collateral slash-escape, pricing (weighted-median), validator-griefing (SPRT). Filed elsewhere: GON-660, GON-335/630, pr1100.

## D. Cosmos-SDK dependency security posture (2026-11-13)

gonka pins **cosmos-sdk v0.53.3** (fork `gonka-ai/cosmos-sdk v0.53.3-ps19-observability`),
**cometbft v0.38.21**, **ibc-go v8.8.0**.

- **All formal advisories through ISA-2025-005 are already fixed in the v0.53.3 base** — ISA-2025-005
  (distribution Validator-Rewards integer-overflow chain-halt, GHSA-p22h-3m2v-cmgh) fixed in v0.53.3;
  ISA-2025-002 (x/group EndBlocker halt) and ASA-2025-003 (groups malicious-proposal halt) predate it.
- **No newer formal advisory (2026) was found affecting v0.53.3.**
- **Gap = post-v0.53.3 patch releases v0.53.4→v0.53.8 (~5 releases, ~13 months).** v0.53.8 (2026-07-27)
  is **state-breaking** and upstream flags "important security fixes" (coordinated upgrade recommended).
  Missing fixes (per gonka issue #1671) are mostly **panic/availability-class**: auth nil-ptr guards in
  `GetSigningTxData` (multisig), reject extra `SignerInfo`s; crypto secp256k1 SEC1 tag-byte check;
  tx/signing index bounds + signer-info/signature count checks; staking redelegation-after-source-removal;
  distribution withdraw strict+fallback; store `traceContext` isolation across branched stores.
- **Tracked as gonka #1671 (OPEN, release-candidate)** — the dev-group rebase to v0.53.8.
- v0.50.x is EOL 2026-03-31; gonka is on v0.53.x so not EOL.

QA note: the missing fixes are malformed-tx → node-panic (availability); a resulting halt would trip
scenario 01 (liveness). When the v0.53.8 rebase lands, test the **coordinated state-breaking upgrade**
(upgrade handler, no apphash divergence, no halt at the boundary) — same discipline as a software-upgrade.
