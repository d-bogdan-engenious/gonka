# PoC / consensus regression coverage map

Purpose: drive **regression automation of the MANUAL QA tests** (live tn3 scenarios + adversarial
hunts). This map is NOT a plan to duplicate dev unit tests — it records where automated coverage
exists today, which manual scenario exercises each consensus area, and what should be automated
(branch-level) next.

Coverage numbers are Go **statement** coverage (`go tool cover`); Go has no native branch coverage,
so "branches" are tracked by enumerating the decision points each scenario must hit (see Branch note).

## Measured statement coverage (this campaign, `-count=1`)

| Package | Coverage | Note |
|---|---|---|
| `x/inference/coefficients` | 97.8% | dynamic-coefficient math (raised from 81.4%) |
| `x/inference/module` | 73.6% | weight/epoch pipeline; consensus fns 100%, epoch-transition fns still 0% (manual-only) |
| `x/inference/keeper` | 60.9% | excludes 2 intentional-RED tests (F1, dangling-model guard) |
| `decentralized-api/poc` | 70.4% | off-chain PoC validator |
| `decentralized-api/mlnodeclient` | 51.1% | mlnode PoC-v2 HTTP client |
| `decentralized-api/broker` | 72.9% | node-number assignment (pkg has a pre-existing flaky test, not ours) |

## Saved tests (15 files, ~78 cases) — authored this campaign

Keep; these fill **dev-uncovered branches** (per fb-qa-test-tiers), they are not the regression goal.
- coefficients: `coefficients_coverage_test.go`, `coefficients_grind_test.go`
- module: `poc_coverage_grind_test.go` (validatedParticipant/calculateParticipantWeight/pocValidated/guardianProtection → 100%), `chainvalidation_helpers_test.go`, `time_normalization_test.go`, `confirmation_poc_lifecycle_coverage_test.go` (CPoC trigger/transition 0→76/80/51%)
- keeper: `random_seed_pruning_test.go` (F1 RED — RandomSeeds unpruned), `dangling_poc_model_halt_test.go` (RED — missing delete guard)
- module (scenario/integration): `dangling_poc_model_formation_test.go` (GREEN — trigger), `dangling_poc_model_endblock_test.go` (GREEN — full EndBlock halt repro)
- dapi: `poc_grind_test.go`, `poc_v2_grind_test.go`, `poc_v2_http_test.go`, `stat_test_params_test.go`, `poc_node_numbers_grind_test.go`

## Consensus areas still covered ONLY by manual QA (automation targets, 0% auto)

These EndBlock/epoch-transition functions are hit live on tn3 every redeploy but have no automated
regression. They are the priority for scenario-level automation:

| Function (module) | Manual scenario doc that exercises it | Automate as |
|---|---|---|
| `BeginBlock` / `EndBlock` epoch-transition dispatch | qa40-gon336-upgrade, qa56-empty-epoch-fallback | Go integration (EndBlock at stage heights) |
| `onSetNewValidatorsStage`, `moveUpcomingToEffectiveGroup` | preserved-nodes, qa56-empty-epoch-fallback | Go integration (validator switch at SetNewValidators) |
| `computePrice` (weighted median) | qa69-dynamic-model-coeff, pricing manual runs | Go integration (proposals → median, branches: no-proposal default, even/odd, weight skew) |
| `getCurrentValidatorWeights` | redeploy valset verification | Go integration |
| `handleExpiredInferenceWithContext`, `expireInferenceAndIssueRefund` | inference expiry/refund manual runs | Go integration (STARTED→EXPIRED + refund) |
| epoch-formation halt paths (CreateEpochGroup/initializeUpcomingEpochModelGroups) | poc-consensus-halt-audit (F2) | DONE — `dangling_poc_model_endblock_test.go` |

## Manual scenario inventory → automation status (consensus/PoC subset)

~50 feature dirs under `docs/testing/features/`. Consensus/PoC-relevant ones and their status:

| Manual scenario | Automatable as | Status |
|---|---|---|
| qa-109-min-epoch-acceptance-weight (floor → valset collapse) | Go integration: seat/guard fallback at floor | TODO |
| qa56-empty-epoch-fallback | Go integration: seatAndGuardParticipants empty → fallback | partial (seat fns covered) |
| poc-weight-count-mismatch | Go integration: SMST count vs validated | TODO |
| gon-455-poc-fee (sybil gas) | Go unit: ChargeStoreCommitGas branches | TODO |
| gon-430-guardian-weightless-vote | covered (guardianProtection 100%) | DONE |
| gon466-freeride / attacker-declared-model | Go integration: validation gate rejects | TODO |
| preserved-nodes / preserved-guardians-pr1179 | Go integration: SamplePreservedForEpisode | partial |
| qa-34-collateral-unbonding-settlement | Go integration: slash reaches unbonding + ordering | TODO (verified-safe by read) |
| qa39/qa40 upgrade + settle wrap | Go integration (pr1100 known) | tracked |
| LIVE-ONLY (deploy, join, PoC-per-epoch verify, gov param change) | scripted testnet probe (epstat.sh family) | manual script lane |

## Branch note

For true branch coverage without a tool, each automated scenario enumerates its decision points as
table-driven subtests (one row per branch), e.g. `computePrice`: {no proposals→default, single
proposer→default-dominated, odd count, even count, weight-skewed, zero-weight→0}. This is how we grow
branch coverage of the MANUAL paths without duplicating dev's statement-level units.

## Next

Pick the automation lane (see chat): (A) in-repo Go integration/scenario tests for the 0% consensus
zones above (CI-runnable, branch-covering), (B) scripted live-tn3 regression checks, (C) prioritized mix.
