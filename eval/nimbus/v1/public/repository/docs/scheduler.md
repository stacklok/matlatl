# Scheduler internals

The scheduler must minimize tenant movement as nodes join and leave. Its exact
contract is intentionally kept in the [placement design](placement-design.md).
The broader context is the [control plane](control-plane.md).
