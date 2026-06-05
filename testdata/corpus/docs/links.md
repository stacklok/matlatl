# Link Showcase

This document exercises P2 link forms for the resolver.

## Valid forms

A valid wikilink to [[guide]] and an aliased wikilink to [[home|the home page]].
A valid relative link to [the overview](sub/overview.md) and a cross-file anchor
to [installation](guide.md#installation).

## Broken forms

A broken wikilink to [[does-not-exist]] and a broken relative link to
[missing](nope.md). A broken anchor to [bad anchor](guide.md#no-such-heading).

## Ambiguous

An ambiguous wikilink to [[notes]] (two documents share that basename).

## Out of root

An out-of-root link to [escape](../../../../etc/passwd) must be a finding, never
read.

## Embed

An embed of ![[guide]] (transclusion).
