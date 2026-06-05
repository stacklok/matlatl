# Directory-link fixture

This corpus exercises directory links (ADR 0008). The link below points at a
*directory*, not a file, with a trailing slash:

- [the ADRs](adr/)

It should resolve Valid (to `adr/README.md`, the directory index) and make the
ADR documents reachable, so none of them is a false orphan.
