# Graceful draining

## Pod lifecycle contract

Once draining begins, readiness becomes false immediately, liveness stays true,
and every new session is rejected. Existing sessions remain valid and may call
`EndSession`; termination waits for them or the operator timeout. A drain is not
a crash and must never make liveness false.

Continue with [health checks](health.md) or [deployment](deployment.md).
