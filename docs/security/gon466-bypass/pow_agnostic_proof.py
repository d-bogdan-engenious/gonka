import sys, types, hashlib
import numpy as np

# Stub torch/tqdm so the REAL random.py imports; its numpy core is untouched.
t = types.ModuleType("torch"); t.float16=np.float16; t.float32=np.float32; t.dtype=object
t.tensor = lambda x, dtype=None: np.asarray(x, dtype=dtype)
t.Tensor=object; t.device=lambda *a,**k:None
nn = types.ModuleType("torch.nn"); nn.Module=object; t.nn=nn
sys.modules["torch"]=t; sys.modules["torch.nn"]=nn
au = types.ModuleType("tqdm.auto"); au.tqdm=lambda x,**k:x
sys.modules["tqdm"]=types.ModuleType("tqdm"); sys.modules["tqdm.auto"]=au

import importlib.util
spec=importlib.util.spec_from_file_location("pow_random","pow_random.py")
r=importlib.util.module_from_spec(spec); spec.loader.exec_module(r)

# Fixed PoW architecture (mlnode Params defaults) — NOT per-model. Only seq_len comes from chain config.
DIM, VOCAB = 2048, 8192
BLOCK_HASH = "0f3ac91bd7e2551122aabbccddeeff00"   # per-epoch, same for everyone
PUBKEY     = "gonka1attacker00000000000000000000000000000"
NONCE      = "42"

def challenge(seq_len):
    """Run the ACTUAL pow challenge generation. Note: NO model argument exists."""
    inp  = np.asarray(r.get_inputs(BLOCK_HASH, PUBKEY, [NONCE], seq_len=seq_len, dim=DIM))
    perm = np.asarray(r.get_permutations(BLOCK_HASH, PUBKEY, [NONCE], dim=VOCAB))
    tgt  = np.asarray(r.get_target(BLOCK_HASH, vocab_size=VOCAB))
    return inp, perm, tgt

def sig(*arrs):
    h=hashlib.sha256()
    for a in arrs: h.update(np.ascontiguousarray(a).tobytes())
    return h.hexdigest()[:20]

# tn3: BOTH models have seq_len=256. Label them differently — the code can't tell.
cheap = challenge(256)   # "Qwen2.5-7B" (seq_len 256)
rich  = challenge(256)   # "Qwen3-4B"   (seq_len 256)
other = challenge(512)   # a hypothetical model with a DIFFERENT seq_len

# Weights are seeded by block_hash only (model_init: get_rng(block_hash,4)) -> model-agnostic
w_a = r.get_rng(BLOCK_HASH,4).standard_normal(4096)
w_b = r.get_rng(BLOCK_HASH,4).standard_normal(4096)

print("=== PoW challenge signature (sha256 of inputs+perm+target) ===")
print("Qwen2.5 (seq_len 256):", sig(*cheap))
print("Qwen3   (seq_len 256):", sig(*rich))
print("other   (seq_len 512):", sig(*other))
print()
print("Qwen2.5 proof == Qwen3 proof :", sig(*cheap)==sig(*rich), " <- forgeable: identical")
print("Qwen2.5 proof == other(512)  :", sig(*cheap)==sig(*other), " <- only seq_len matters, not model")
print("PoW weights (block_hash-seeded) identical:", np.array_equal(w_a,w_b))
