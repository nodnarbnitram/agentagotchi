# Terminal Task Presence retention

A Terminal Task Presence is retained until it is acknowledged, with two bounds
so an Edge Bridge that outlives its Harness Adapters does not accumulate dead
notifications: a 7-day TTL measured in monotonic time (configurable per Edge)
and a per-Edge FIFO bound of ~200 terminal presences (oldest evicted first).
Expiry or eviction removes the presence and publishes a new revision exactly as
acknowledgement does — it never rewrites the presence to `completed` or
`failed` — and is visible in administration diagnostics.

We chose this over acknowledge-only retention (simple, but unbounded state
after adapter deaths) and over short TTLs (which would delete overnight results
before the user sees them). The TTL uses monotonic time because HANDOFF forbids
wall-clock comparisons for liveness decisions.
