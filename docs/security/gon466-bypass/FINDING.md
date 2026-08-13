# GON-466 bypass: PoC provenance is forgeable because Proof-of-Work is model-agnostic

**Severity: HIGH.** GON-466 (PR product-science/gonka#79) does not close the weight-inflation
vector it targets — it relocates it. The fix stops trusting the self-reported *hardware* label
and starts trusting the PoC-commit `ModelId` label, but both are self-declared and backed by the
**same model-agnostic proof**, so a participant can still seat compute weight in a richer model
than they ever served for.

## What GON-466 does (recap)
`setModelsForParticipants` seats each validated ML node into the model bucket it was "proven"
under, and uses the hardware inventory only as a filter (drop a node whose model the hardware no
longer declares). It never lets the hardware label *move* a node to another model. Verified: the
relabel attack is blocked (see `gon466_bypass_test.go`, vectors V1/V2/V4 pass = blocked). Deployed
and confirmed live on testnet-3 as `v0.2.16-gon466`.

## The bypass
The "proven model" GON-466 trusts comes from the PoC-commit `ModelId`, which is validated by other
validators re-running the proof. But the proof is **Proof-of-Work over a fixed synthetic network**,
not the real model:

- `pow.compute.Compute.__init__(params, block_hash, block_height, public_key, r_target, devices, node_id)`
  takes **no `model_id`**.
- Weights: `ModelWrapper.build(hash_=block_hash, ...)` → `torch.manual_seed(42)` +
  `initialize_model_with_pool(model, str(block_hash))`. Seeded by **block hash**, never the real
  model. Architecture is a small fixed llama (dim≈1024–2048), not Qwen.
- Challenge: `get_inputs / get_permutations / get_target` (pow/random.py) seed only on
  `block_hash + public_key + nonce` and numeric params (`seq_len`, `dim`, `vocab_size`).
- Chain `PoCModelConfig` carries only `seq_len` + `stat_test` per model. On testnet-3 both models
  are byte-identical: `seq_len=256`, `stat_test{dist_threshold=0.4, p_mismatch=0.1, p_value_threshold=0.05}`.
- Chain records validator verdicts only; `msg_server_poc_v2_commit` accepts any `ModelId` that is a
  governance model. No model binding anywhere on-chain.

Therefore the proof a node generates while "serving Qwen2.5-7B" is **identical** to one "for
Qwen3-4B". Empirically confirmed by running the real `random.py` (see `pow_agnostic_proof.py`):

```
Qwen2.5 (seq_len 256) challenge sha256: d32a7c2fb258d1dcd8bd
Qwen3   (seq_len 256) challenge sha256: d32a7c2fb258d1dcd8bd   <- IDENTICAL
other   (seq_len 512) challenge sha256: 5b97bcc1c9282c8c0e5b   <- only seq_len matters
weights (block_hash-seeded) identical: True
```

### End-to-end exploit
1. Do the ordinary (model-agnostic) PoW once.
2. Commit it under the **rich** model — free, the label is unverified.
3. Declare hardware as the rich model too.
4. GON-466's filter sees commit-model == hardware-model → **seats the node in the rich bucket**,
   earning its (~3x) coefficient for compute never spent on that model.

The original attack moved one label (hardware) and got caught. This moves *both* labels together,
which the fix permits because the commit label is just as unverified as the hardware label was.

Proven on real chain code in `gon466_exploit_test.go`: a rich-model commit + one honest rich-model
validator's verdict → `PoCWeightCalculator.Calculate()` mints rich weight → `setModelsForParticipants`
seats it in the rich bucket.

## Gating condition (why not trivially exploitable by a lone actor today)
`pocValidated` (chainvalidation.go) needs the rich model to have per-model voting power. testnet-3's
`Qwen3-4B` subgroup is empty, so a lone attacker's rich commit is rejected. Full exploitation needs
the rich model to have validators — either an already-served rich model, or a coordinated set that
bootstraps it (mutually validating their identical proofs). Since PoW cost is identical for any
model, there is **zero economic deterrent** to that migration. The planned live reproduction adds 3
servers serving the second model to provide those validators; then any existing node piggybacks with
a model-agnostic proof.

## Recommendation
Bind the proof to the model. Options:
- Derive the PoW seed/params from the model identity (so a Qwen3 proof is not a valid Qwen2.5 proof).
- Or gate the model bucket / coefficient on **inference-serving** validation (actually answering that
  model's queries), so `ModelId` cannot be a free label.

Without one of these, GON-466's provenance guarantee is only as strong as an unverified label.

## Artifacts
- `gon466_bypass_test.go` — adversarial vectors against the fix (V1/V2/V4 blocked; V3 amplifier).
- `gon466_exploit_test.go` — end-to-end mint+seat of rich weight on real chain code.
- `pow_agnostic_proof.py` — runs the real `pow/random.py`; shows identical proof across models.
- `pow_random_vendored.py` — vendored copy of `mlnode/packages/pow/src/pow/random.py` for the proof.

Run: `go test ./x/inference/module/ -run 'TestBypass_|TestExploit_' -v` and
`python pow_agnostic_proof.py` (needs numpy).
