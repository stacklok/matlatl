---
title: "Navigability ideas for matlatl, in plain terms"
matlatl: orphan-intentional
---

# What matlatl could do next, explained simply

> A plain-language companion to
> [information-organization-theory.md](information-organization-theory.md), which
> has the full history, citations, and implementation notes. This page is the
> "what and why" without the jargon.
>
> Marked `orphan-intentional` so matlatl doesn't flag its own research note.

matlatl looks at all the markdown docs in a repo as a **network**: each doc is a
dot, each link is a thread between dots. Everything below is just a different
question you can ask about that network. For each: what it finds, a real example,
and why you'd care.

Today matlatl tells you what's **broken** (dead links, orphaned files). All of this
is about what's **findable** — how easy your docs are to navigate, for people *and*
for AI agents. Nearly all of it is free math on the graph matlatl already builds: no
new dependencies, and no loss of its "same input → identical output" guarantee.

---

## The big one: smarter "you should link these two docs"

**Today:** matlatl finds clumps of docs that are completely cut off from each other
and flags every pair. It's noisy, and you can't tell which gap actually matters.

**The upgrade:** ask "do these two docs *look like* they're about the same thing,
based on who they link to?"

- **Example:** `auth-setup.md` and `auth-troubleshooting.md` never link each other —
  but they *both* link to `auth-overview.md` and `config.md`. That shared pattern is
  a strong tell they belong together.
- **Benefit:** Instead of 200 noisy "disconnected island" warnings, you get a short
  ranked list: *"these specific docs are probably about the same topic and should
  link to each other."* Actionable, not noise. Highest payoff of everything here.

This is the same math librarians have used since the 1960s to tell which research
papers are related — pointed at your docs.

## A health score for your docs ("compactness" + "stratum")

Two simple 0-to-1 numbers describing the *shape* of your whole doc set.

- **Compactness** = how tightly everything is connected. Near 0 = scattered islands,
  hard to get around. Near 1 = everything links to everything (a "hairball" where
  links stop meaning anything). There's a healthy middle.
- **Stratum** = how much of a *reading order* your docs have. High = a sequence, like
  a tutorial (read 1, then 2, then 3). Low = a reference wiki you dip into anywhere.

- **Example:** compactness drops release over release — a sign your docs are
  fragmenting as the team adds pages without linking them in.
- **Benefit:** one glanceable number for "are my docs getting easier or harder to
  navigate," instead of eyeballing a diagram. Great as a CI trend.

## "How many clicks to get anywhere?"

The average number of link-hops between any two docs, plus how "cliquey" they are.

- **Example:** a doc is reachable, but it's 8 clicks from the README. Effectively,
  nobody (and no agent) will find it.
- **Benefit:** flags docs that are *far away* even if not broken — the "yes it
  exists, but good luck finding it" problem.

## Better orphan detection (graduated, not all-or-nothing)

**Today:** flags a doc only if it has zero links in **and** zero links out.

**The upgrade:** separate two different problems with different fixes:

- **Orphan** = nothing links *to* it → link it in from somewhere relevant. (And tier
  it: one incoming link is fragile; 3+ is healthy.)
- **Dead-end** = no links *going out* → add onward links so readers aren't stranded.

- **Example:** a new `migration-guide.md` is linked from the README (not an orphan)
  but links nowhere itself — a reader lands there and hits a wall.
- **Benefit:** catches more real problems, each with the *right* fix. This is exactly
  how Wikipedia manages 6M+ articles.

## "Which docs are load-bearing?" (single points of failure)

Find the docs that act as *bridges* — the ones most paths route through.

- **Example:** 60% of navigation routes through `architecture.md`. Delete it or break
  its links and half your docs become unreachable.
- **Benefit:** tells you which docs are critical infrastructure — protect them, review
  changes carefully. Different from "most popular"; a humble glue page can be the
  critical bridge.

## A map of your docs' structure ("bow-tie")

Sort every doc into a few buckets relative to the well-connected core:

- **Core** — the navigable heart; get anywhere from anywhere.
- **Entry pages** — feed into the core, but the core doesn't link back.
- **Terminal pages** — reachable from the core, but they dead-end.
- **Disconnected** — floating loose.

- **Benefit:** an agent or new teammate gets "here's the core, start there," instead
  of a flat list of 300 files. Instant orientation.

## Do your links tell you where they go? ("scent")

Check whether a link's *text* hints at what's on the other side.

- **Example:** `See [here](setup.md)` tells you nothing; `See the [setup
  guide](setup.md)` does. Same for bare URLs and "click this."
- **Benefit:** especially important for **AI agents** — an agent has no mouse-hover,
  no visual cues. The link text is *all* it has to decide whether to follow a link.
  matlatl can flag every low-information link and say "rename this to name its
  target."

## A guided reading order for agents ("trails") — the exciting one

Instead of dumping all docs at once, generate a smart *order* to read them: start at
the README, hit the most important hubs early, follow the natural structure.

- **Example:** an AI coding agent joins your repo. Hand it *"read these 9 docs in this
  order to understand this project"* instead of making it read everything or guess.
- **Benefit:** the whole "docs for agents" pitch made real — an optimal onboarding
  path. (It's actually a 1945 idea — Vannevar Bush's "associative trails" — finally
  useful now that agents are the readers who benefit most.)

## The smaller wins

- **PageRank** — a second, steadier way to rank "most important docs" (the algorithm
  that built Google), alongside the one matlatl already has.
- **Backlinks in the output** — every doc gets a "linked from:" list, so you see what
  points *at* a page, not just what it points to (like Obsidian/Roam).
- **Semantic similarity** — the most powerful "these are related" detector, using AI
  embeddings. Catch: it's not perfectly reproducible run-to-run, which fights
  matlatl's "same input → identical output" guarantee. So it stays an opt-in extra,
  never the default — the way external-link-checking is handled today.

---

**The through-line:** matlatl already knows what's *broken*. All of this is about
what's *findable* — turning it from a link checker into something that tells you (and
your agents) how easy your docs are to navigate, what's missing, what's critical, and
where to start reading.
