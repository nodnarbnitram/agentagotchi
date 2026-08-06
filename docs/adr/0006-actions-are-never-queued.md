# Device actions are never queued

A device action either dispatches immediately along a valid presence route —
direct Edge, or Home to the origin Edge — or fails unavailable. Nothing is
stored for later delivery at any hop, and acknowledgement happens only after
exact harness capability success, deduplicated by action ID at the origin Edge.

We chose this over store-and-forward because a delayed focus, approval, or
response acts on a context that may no longer exist — unsafe in exactly the
cases (approvals, permissions) where correctness matters most. A failed-fast
action with idempotent user retry is safer than a queued action firing against
stale state. The cost is a manual retry on flaky links, and a future reader
should not "fix" this by adding a queue.
