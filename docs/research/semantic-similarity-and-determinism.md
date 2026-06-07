---
title: "The semantic frontier: content-based document similarity and the determinism boundary"
matlatl: orphan-intentional
---

# The semantic frontier: content-based similarity, and why it lives behind a flag

> Status: research note for [P12 — Semantic frontier](https://github.com/stacklok/matlatl/issues/6),
> not a binding decision. It is the literature-and-engineering companion to
> [information-organization-theory.md](information-organization-theory.md) §4-L,
> which placed embedding-based "topically-similar-but-unlinked" detection at the
> bottom of the roadmap as the one deliberately **non-deterministic, flag-gated**
> frontier item. This note answers *why* that boundary falls where it does, what
> the field actually settled, and how matlatl should stage the feature.
>
> Marked `orphan-intentional` so matlatl doesn't flag its own research note.

## 0. The thesis

Content similarity — *"are these two documents about the same thing?"* — is a
**70-year-settled** problem. matlatl already answers the *structural* version of
"these two docs belong together" deterministically: topology-based link
prediction (Adamic/Adar + bibliographic coupling + co-citation,
[ADR 0013](../adr/0013-topology-link-prediction.md), `internal/domain/graphmodel/linkprediction.go`).
P12 adds the **content** axis — the signal topology cannot see: two docs whose
*prose overlaps heavily* but that share no graph neighbours.

The single most important finding of this note is that a clean **determinism
fault line** runs straight through the content-similarity literature, and it
falls exactly where matlatl's existing `--check-external` boundary
(`internal/application/pipeline.go`, the opt-in network-liveness check) already
sits:

- The **lexical half** (TF-IDF + cosine) is **byte-stable and golden-testable**.
- The **dense-embedding half** is **non-deterministic by construction** — and,
  crucially, *not* merely because of float rounding, but because production
  embedding kernels are not batch-invariant and the model landscape is itself
  non-stationary.

P12 is therefore not a research gamble. It is a **staging-and-quarantine**
exercise: ship the deterministic lexical tier behind a flag; quarantine dense
embeddings as a further, explicitly-non-deterministic opt-in, exactly like
`--check-external` keeps network results out of the golden path.

---

## 1. The lineage of content-based similarity

Every method below reduces "find related documents" to the same shape Salton
gave it in 1975: represent each document as a vector, then rank pairs by a
similarity measure. They differ in *what the vector's dimensions mean* and
*whether the computation is reproducible*.

| Method | What it computes | Strength for "same topic?" | Deterministic? |
| --- | --- | --- | --- |
| **VSM** — Salton, Wong & Yang 1975 (CACM 18(11):613–620) | documents as vectors in a term/property space | the *frame* for everything below | **Yes** |
| **TF-IDF** — Spärck Jones 1972 (IDF); Salton & Buckley 1988 (the SMART weighting notation) | term weight `tf · log(N/df_t)` — highest for terms frequent in *few* docs | strong, interpretable lexical baseline | **Yes**, given fixed tokenization |
| **Cosine similarity** — Salton/SMART; Manning et al. IR-book ch.6 | length-normalized dot product of weight vectors | the standard similarity measure; "more-like-this" = highest-cosine pairs | **Yes** |
| **BM25** — Robertson & Zaragoza, *Foundations & Trends in IR* 3(4):333–389 | TF-IDF **plus term-frequency saturation** + document-length normalization | beats raw TF-IDF chiefly when one term repeats *many times in one long doc* | **Yes** |
| **LSI / LSA** — Deerwester, Dumais, Furnas, Landauer & Harshman 1990; Landauer & Dumais 1997 | truncated SVD of the term-document matrix → `k` latent dimensions | captures synonymy / latent topics that raw VSM misses | **⚠️ only with a pinned SVD sign convention** (see §4) |
| **pLSA / LDA** — Hofmann 1999; Blei, Ng & Jordan 2003 | probabilistic topic mixtures | richer topic structure | **No** (random init / sampling) |
| **Dense embeddings** — word2vec (2013) → GloVe (2014) → doc2vec (2014) → **Sentence-BERT** (Reimers & Gurevych, EMNLP 2019) → **E5 / BGE / GTE / Nomic / OpenAI text-embedding-3** (2022–2026) | learned dense vectors; bi-encoder cosine | **strongest** — catches semantic relatedness no lexical method sees | **No** (see §3) |

Two facts from this lineage are load-bearing for matlatl:

- **The bi-encoder is what makes corpus-scale similarity tractable.** A
  cross-encoder BERT over 10,000 sentences requires `n(n−1)/2 ≈ 50M` inferences
  (~65h on a V100); Sentence-BERT's siamese architecture produces a *fixed
  vector per document* compared by cosine, collapsing the same task to ~5s with
  comparable accuracy (Reimers & Gurevych, arXiv:1908.10084). matlatl's all-pairs
  use case *requires* a bi-encoder embedding model, never a cross-encoder
  reranker.
- **Dense embeddings demonstrably surface relatedness lexical methods miss.** E5
  (Wang et al., arXiv:2212.03533) was the first model to beat the strong BM25
  baseline *zero-shot* on the BEIR benchmark with no labelled data. That is the
  clearest evidence that the content signal P12 adds is real and not subsumed by
  TF-IDF. (Caveat: that result is retrieval nDCG@10, a defensible but not exact
  proxy for "should-link detection.")

### Why plain TF-IDF, not BM25, for the lexical tier

BM25's one defining advantage over TF-IDF is **term-frequency saturation**: a
single term's contribution is capped by an asymptotic ceiling no matter how
often it repeats (Robertson & Zaragoza). That advantage pays off when a term
recurs *many times within one long document*. matlatl's lexical tier is proposed
over **headings** (and optionally short doc bodies), where intra-document term
repetition is low and BM25's document-length model (`avgdl`) adds machinery for
little gain. Plain TF-IDF cosine is the simpler, equally-defensible deterministic
choice; BM25 is a possible refinement, not a prerequisite.

---

## 2. Semantic vs topological link prediction (and an honest caveat)

matlatl already ships the **topological** axis ([ADR 0013](../adr/0013-topology-link-prediction.md)):
`PredictLinks` scores unlinked pairs by Adamic/Adar (primary), bibliographic
coupling `|out(A)∩out(B)|`, and co-citation `|in(A)∩in(B)|`. The **content** axis
is genuinely orthogonal: `auth-setup.md` and `auth-troubleshooting.md` that share
heavy prose overlap but *no graph neighbours* are invisible to Adamic/Adar yet
obvious to TF-IDF cosine. Conversely, two docs that co-occur under many shared
hubs but use disjoint vocabulary are caught by topology and missed by content.
The two signals answer different questions and neither dominates.

**Calibration caveat — do not over-claim.** The literature that content-based
link prediction *exists* is solid (e.g. text-feature Wikipedia link prediction,
Tran et al., arXiv:2309.00317), but the research behind this note could not find
a strong primary source proving that a **hybrid topology+content predictor beats
topology alone** on a documentation corpus. The one source touching it directly
is a weak competition entry with a suspiciously perfect reported F1, likely from
label leakage. The honest framing for matlatl is therefore: **P12 is a
complementary signal that catches a different failure mode**, not a measured
improvement over [ADR 0013](../adr/0013-topology-link-prediction.md). Whether the
content signal surfaces real should-link pairs the existing Adamic/Adar signal
misses is an **empirical question to validate on the dogfood corpus**, not an
established result to assert in an ADR.

---

## 3. The determinism crux (the corrected story)

This is the heart of why P12 is flag-gated. The intuitive explanation —
*"floating-point addition is non-associative, therefore embeddings are
non-deterministic"* — is **not** the foundational cause, and stating it that way
is wrong. The verified picture (Thinking Machines, "Defeating Nondeterminism in
LLM Inference," 2025; corroborated by LMSYS/SGLang and ORNL SC24,
arXiv:2408.05148):

- **The dominant practical driver is non-batch-invariant kernels.** A single
  document's embedding numerics change with the *concurrent batch composition* —
  i.e. what *else* happened to be in the inference batch. Floating-point
  non-associativity in parallel reductions (notably GPU atomic adds) is the
  *enabling substrate*, not the headline cause.
- **Bitwise determinism is recoverable but costly.** Enforcing batch-invariant
  RMSNorm / matmul / attention kernels makes inference bit-reproducible, at a
  measured ~20% kernel cost (and ~1.6–2.1× system-level slowdown). So
  reproducibility is *possible* — but it pins you to a specific runtime and
  pays a real performance tax.
- **Even bit-reproducible inference stays model-dependent.** MTEB (Muennighoff
  et al., arXiv:2210.07316) found that **no single embedding model dominates
  across all task types**. So *which pairs get flagged* depends on the chosen
  model, and the model landscape (E5 / BGE / GTE / Nomic / OpenAI
  text-embedding-3) turns over quarterly. The dense output is irreducibly
  model-bound regardless of float determinism — which by itself disqualifies it
  from a byte-stable contract.

### The determinism taxonomy

| Tier | Determinism status |
| --- | --- |
| **TF-IDF cosine**, fixed tokenization | **Byte-stable. Golden-testable.** Integer/rational term counts; order-stable float sums via matlatl's existing sorted-accumulation discipline. |
| **LSA / truncated SVD** | **Byte-stable *only* with (a) a pinned singular-vector sign convention (§4) and (b) a full deterministic SVD, not an iterative Lanczos/randomized solver.** |
| **Dense embeddings** | **Non-deterministic by construction** — batch/kernel non-invariance *and* model-version drift. Never default; never golden-tested. |

The practical consequence: "just sum in a fixed order" *rescues TF-IDF* (and is
already how matlatl keeps the Adamic/Adar float sum byte-stable) but **does not
rescue dense embeddings** — their nondeterminism is upstream of the order in
which matlatl would add anything.

---

## 4. The SVD sign-ambiguity trap (the one actionable LSA detail)

If matlatl ever adds an LSA tier, this is the load-bearing gotcha and the single
most important clause for the determinism-boundary ADR. From the canonical
reference — Bro, Acar & Kolda, "Resolving the Sign Ambiguity in the SVD"
(Sandia SAND2007-6422; *Journal of Chemometrics* 2008, 22(2):135–140):

> the decomposition is only unique up to a reflection of each set of singular
> vectors … The actual sign is determined as a by-product of the computations
> that are used to ensure numerical stability. This determination of sign is
> essentially the same as assigning the sign **randomly**.

Mathematically, `X = U D Vᵀ = (U D′) D (V D′)ᵀ` for any diagonal sign matrix
`D′`, so the factorization is valid with either sign. Two SVD routines on
*identical input* can legitimately return opposite-signed singular vectors
(MATLAB's ARPACK-based `svds` flips signs relative to LAPACK-based `svd`). This
means an LSA tier's vectors — and therefore its cosines — are **not byte-stable
across platforms / BLAS versions** unless a sign convention is pinned. The fix is
one deterministic rule: e.g. force the largest-magnitude loading in each singular
vector to be positive (exactly what scikit-learn's `svd_flip` does). This, plus
using a *full* (non-iterative) SVD to avoid solver-convergence sensitivity, is
what moves LSA from "⚠️" to "deterministic." It is the reason this note ranks a
plain TF-IDF tier *above* an LSA tier in priority.

---

## 5. Thresholding and evaluation (what the literature does not hand you)

There is no universal cosine threshold that means "these should link." The
research could not surface a quantified, transferable cutoff, precision/recall
curve, or false-positive-control recipe for heading-level document similarity.
The implication for matlatl is concrete:

- **Derive the threshold empirically on the dogfood corpus**, and expose it as a
  tunable knob (mirroring the config-only `linkSuggestionMinShared` floor in
  [ADR 0013](../adr/0013-topology-link-prediction.md)), with a conservative
  default chosen for precision.
- **Default to precision over recall.** The failure mode that destroys trust is
  drowning the user in weak "these are vaguely similar" suggestions. Cap the
  output (as `MaxSuggestedLinks` already does), rank by similarity DESC, and
  surface only a short top-N — matlatl's house style for the existing
  experimental signals.
- **Evaluate by link recovery.** The standard offline evaluation for
  related-document detection is to hide a fraction of real links and measure how
  many the method recovers (the "see also" / link-recovery experiment shape).
  matlatl's own repo plus a handful of public docs corpora are a reasonable test
  bed.

---

## 6. Go ecosystem practicalities (treat as an unvalidated spike)

The web research did not yield verified, citable claims on Go-specific
dependency-weight / binary-size / offline-CI tradeoffs, so the following is
first-principles guidance, not a settled recommendation — it needs a dedicated
spike before any Tier-2 commitment.

- **Tier 1 (TF-IDF cosine): pure Go, zero heavy dependencies.** Tokenize → term
  counts → IDF → cosine is a few hundred deterministic lines, entirely within the
  hand-rolled discipline [ADR 0002](../adr/0002-library-choices.md) already
  applies to the graph layer. No new dependency is justified.
- **Tier 1b (LSA): `gonum` provides a deterministic full SVD.** This is the only
  new dependency an LSA tier needs, and it is a well-maintained,
  numerically-serious library. The sign convention (§4) is matlatl's
  responsibility on top of it. (`james-bowman/nlp` builds LSI on gonum and is a
  useful reference implementation.)
- **Tier 2 (dense embeddings): pulls in heavy, non-deterministic infrastructure.**
  Either a local ONNX runtime (e.g. via `knights-analytics/hugot`) — large
  native dependency, platform-specific, but offline — or an external embedding
  API — small client, network-dependent, and a data-egress consideration under
  [ADR 0003](../adr/0003-security-model.md). Both reinforce "further opt-in
  only," and the model id/version must be recorded in output so a reader knows
  the result is model-bound.

---

## 7. Recommendation: how matlatl should stage P12

Three tiers, mapped onto patterns already in the codebase:

**Tier 0 — default path: unchanged.** No semantic analysis runs by default. This
is the non-negotiable core of the issue.

**Tier 1 — `--semantic` (deterministic TF-IDF-over-headings cosine).** A new
analyzer emitting an **additive, Info-severity signal that never gates `check`**
— structurally identical to the existing `suggested-link`
([ADR 0013](../adr/0013-topology-link-prediction.md)) and excluded from the
exit-code contract ([ADR 0005](../adr/0005-exit-code-contract.md)) the same way
`knowledge-gap` is. Reuse the byte-stability discipline already in
`linkprediction.go`: sorted iteration, float sums accumulated in sorted order.
Because it is deterministic it *could* technically join the default path, but the
issue's design intent is to flag-gate the whole semantic frontier, so it stays
behind the flag for a clean conceptual boundary. **This is the recommended first
— and possibly only — shipped tier.**

**Tier 1b — optional LSA** *only if* Tier 1 proves insufficient: gonum full SVD
plus a **pinned sign convention** (§4), documented in the ADR. Lower priority;
adds a dependency and a determinism surface for a marginal recall gain.

**Tier 2 — dense embeddings: a separate, explicitly-non-deterministic opt-in.**
The true `--check-external` analogue: results appended only when the flag is set,
**excluded from golden tests**, with the model id + version recorded in output.
Never default.

### What the determinism-boundary ADR should say

1. **State the boundary explicitly:** lexical TF-IDF cosine over a fixed
   tokenization is byte-stable and lives on the golden path; LSA/SVD is byte-stable
   *only* with a pinned singular-vector sign convention and a deterministic
   (non-iterative) SVD; dense embeddings are non-deterministic by construction
   and are quarantined exactly like `--check-external` network results
   ([ADR 0003](../adr/0003-security-model.md)).
2. **Cite the sign-ambiguity source** (Bro, Acar & Kolda) and name the chosen
   convention if an LSA tier ships.
3. **Record the non-portability fact** (MTEB: no model dominates) as the rationale
   that dense output is model-bound and unfit for a determinism contract *even if*
   made bitwise-reproducible.
4. **Correct the myth in prose:** the dominant cause of embedding nondeterminism
   is batch/kernel non-invariance, not floating-point non-associativity per se —
   so order-stable summation does not rescue dense embeddings (it does rescue
   TF-IDF).
5. **Do not claim a measured win over [ADR 0013](../adr/0013-topology-link-prediction.md).**
   Frame P12 as complementary and flag empirical validation as future work.

---

## 8. Open questions

These are not settled by the literature and must be answered by matlatl's own
experiments before committing past Tier 1:

1. **Marginal value over topology.** Does the content signal demonstrably surface
   should-link pairs that matlatl's existing Adamic/Adar / co-citation /
   bibliographic-coupling predictor misses on real repos — or is the benefit only
   asserted from general literature?
2. **Threshold.** What cosine cutoff meaningfully marks "should link" for
   heading-level documents, and what precision/recall does it buy on the dogfood
   corpus?
3. **Document granularity.** Per-file vs per-section embedding, and headings-only
   vs short-body input — which gives the best precision for the lowest noise?
4. **Go Tier-2 path.** The concrete dependency-weight / binary-size / offline-CI
   tradeoffs between a bundled ONNX runtime and an embedding API are unvalidated.

---

## 9. Primary sources

- Salton, G., Wong, A., Yang, C. S. (1975). *A Vector Space Model for Automatic
  Indexing.* CACM 18(11):613–620.
- Spärck Jones, K. (1972). *A statistical interpretation of term specificity and
  its application in retrieval.* Journal of Documentation 28(1).
- Salton, G., Buckley, C. (1988). *Term-weighting approaches in automatic text
  retrieval.* Information Processing & Management 24(5) — the SMART notation.
- Manning, C., Raghavan, P., Schütze, H. *Introduction to Information Retrieval*,
  ch. 6 (vector space model, TF-IDF, cosine). nlp.stanford.edu/IR-book.
- Robertson, S., Zaragoza, H. (2009). *The Probabilistic Relevance Framework:
  BM25 and Beyond.* Foundations & Trends in IR 3(4):333–389.
- Deerwester, S., Dumais, S., Furnas, G., Landauer, T., Harshman, R. (1990).
  *Indexing by Latent Semantic Analysis.* JASIS 41(6).
- Landauer, T., Dumais, S. (1997). *A solution to Plato's problem: the latent
  semantic analysis theory of acquisition.* Psychological Review 104(2).
- Hofmann, T. (1999). *Probabilistic Latent Semantic Indexing.* SIGIR.
- Blei, D., Ng, A., Jordan, M. (2003). *Latent Dirichlet Allocation.* JMLR 3.
- Reimers, N., Gurevych, I. (2019). *Sentence-BERT.* EMNLP. arXiv:1908.10084.
- Wang, L. et al. (2022). *Text Embeddings by Weakly-Supervised Contrastive
  Pre-training* (E5). arXiv:2212.03533.
- Nussbaum, Z. et al. (2024). *Nomic Embed: Training a Reproducible Long Context
  Text Embedder.* arXiv:2402.01613.
- Muennighoff, N. et al. (2023). *MTEB: Massive Text Embedding Benchmark.* EACL.
  arXiv:2210.07316.
- Tran, A., Nguyen, P., Luu, S. (2023). *A Text-based Approach for Link Prediction
  on Wikipedia Articles.* arXiv:2309.00317.
- Bro, R., Acar, E., Kolda, T. (2008). *Resolving the Sign Ambiguity in the SVD.*
  Sandia SAND2007-6422; Journal of Chemometrics 22(2):135–140.
- He, H. et al. (2025). *Defeating Nondeterminism in LLM Inference.* Thinking
  Machines; corroborated by LMSYS/SGLang (2025).
- ORNL (2024). *Impacts of floating-point non-associativity on reproducibility.*
  arXiv:2408.05148.
