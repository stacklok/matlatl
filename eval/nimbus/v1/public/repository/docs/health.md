# Health checks

Readiness controls new traffic; liveness says the process can continue draining.
The exact state transition lives in [graceful draining](draining.md). Kubernetes
wiring is in [deployment](deployment.md).
