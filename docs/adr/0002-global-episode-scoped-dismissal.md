# Global, episode-scoped dismissal of Task Presences

Users can dismiss a Task Presence without mutating the Harness Session, and the
semantics differ by liveness. A Terminal Task Presence (`ready`/`blocked`) is
**acknowledged**: the owning Edge Bridge removes it from every Presence Feed.
An input-gated presence (`needs_input`) is **snoozed**: it stays in the feed
and task list but stops claiming the Featured Task. Both are recorded once,
globally, at the owning Edge Bridge, and converge everywhere by normal snapshot
replacement; per-device dismissal state is deliberately deferred. Both reset
when the presence's state or reason changes, so a genuinely new completion or
approval request surfaces again — while repeated identical reports do not.

We chose Edge-global dismissal over per-device state so all surfaces agree and
firmware stays simple, and snooze over removal for input gates because removal
would misrepresent a Harness Session that is still waiting (the adapter's next
absolute report would resurrect it). Known limitation accepted for v1: an
identical back-to-back approval request on a snoozed task produces no state or
reason change, so it stays snoozed; an adapter-provided opaque attention-epoch
counter is the designed fix if real usage requires it. No time-based resurface
(nagging) in this rebuild.

Implementation consequence: acknowledgement and snooze are deliberate,
protocol-versioned device-to-Edge control messages defined in
`docs/PROTOCOL.md`; neither dispatches to a Harness Adapter capability.
