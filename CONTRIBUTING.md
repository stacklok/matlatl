# Contributing to doctopus

Thanks for contributing! Start with the **[developer guide](docs/dev-guide.md)** —
it covers the repo layout, the DDD layering rules, and the testing strategy. The
**[ADRs](docs/adr/README.md)** record the decisions you must not silently
override; read the relevant one before changing load-bearing behavior.

## Verification gate

Every change must pass the full local suite before review (this is what CI runs):

```console
go build ./... && go vet ./... && \
go test -race -count=1 ./... && \
go test -tags=integration -race -count=1 ./... && \
gofmt -l . && golangci-lint run
```

Plus the **domain-purity** guard (ADR 0004) — it must print **nothing**:

```console
go list -deps ./internal/domain/... | \
  grep -E 'dominikbraun|goldmark|spf13|sabhiram|mark3labs|net/http|internal/(application|infrastructure)'
```

`make check` runs fmt + vet + lint + unit tests; `make test-integration` runs the
integration/golden suite. If you touch markdown with links, keep the doc gate
green: `make dogfood` regenerates `llms.txt` and runs `doctopus check . --strict`.

## Conventions

- Keep the **domain** pure and **determinism** intact (sorted iteration; artifacts
  are byte-stable and golden-tested).
- If you change a machine artifact's shape, update its schema under
  `docs/schemas/` and bump the schema version.
- Keep human-curated docs short and self-contained; supersede ADRs rather than
  editing accepted ones.
