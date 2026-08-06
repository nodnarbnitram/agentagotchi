// Tests for the Home Bridge pure modules: presence merge and pairing.
import assert from "node:assert/strict";

const { HomePresence } = await import("../src/presence.ts");
const { PairingCeremony } = await import("../src/pairing.ts");
const { validateUpstreamSnapshot, validateFeedTask } = await import("../src/wire.ts");

let passed = 0;
async function t(name, fn) {
  try {
    await fn();
    passed++;
    console.log("ok -", name);
  } catch (err) {
    console.error("FAIL -", name, err);
    process.exitCode = 1;
  }
}

const task = (id, overrides = {}) => ({
  taskPresenceId: id,
  safeTitle: "Codex",
  state: "running",
  reason: "working",
  subagentCount: 0,
  capabilities: ["focus"],
  updatedAt: "2026-01-01T00:00:00.000Z",
  snoozed: false,
  ...overrides,
});

const snapshot = (edgeId, generation, revision, tasks) => ({
  schema: "agentagotchi.upstream.v1",
  type: "snapshot",
  edgeId,
  generation,
  revision,
  snapshotGeneratedAt: "2026-01-01T00:00:00.000Z",
  tasks,
  counts: { needsInput: 0, blocked: 0, ready: 0, running: tasks.length },
  aggregateState: "running",
});

const UUID_A = "019fa063-b4d1-7d81-bced-7f9f55ec7611";
const UUID_B = "019fa063-b4d1-7d81-bced-7f9f55ec7612";

await t("snapshot replaces only that edge's contribution", () => {
  const home = new HomePresence("home-1");
  home.applySnapshot(snapshot("edge-a", 1, 1, [task(UUID_A)]));
  home.applySnapshot(snapshot("edge-b", 1, 1, [task(UUID_B, { safeTitle: "Pi" })]));
  // edge-a resends absolute with its task now ready.
  home.applySnapshot(snapshot("edge-a", 1, 2, [task(UUID_A, { state: "ready", reason: "completed" })]));
  const merged = home.mergedTasks();
  assert.equal(merged.length, 2);
  const a = merged.find((m) => m.taskPresenceId === UUID_A);
  const b = merged.find((m) => m.taskPresenceId === UUID_B);
  assert.equal(a.state, "ready");
  assert.equal(b.state, "running"); // untouched by edge-a's replacement
});

await t("stale and replayed snapshots rejected", () => {
  const home = new HomePresence("home-1");
  assert.equal(home.applySnapshot(snapshot("edge-a", 2, 5, [task(UUID_A)])), true);
  assert.equal(home.applySnapshot(snapshot("edge-a", 2, 5, [task(UUID_A, { state: "ready" })])), false);
  assert.equal(home.applySnapshot(snapshot("edge-a", 2, 3, [task(UUID_A)])), false);
  assert.equal(home.applySnapshot(snapshot("edge-a", 1, 99, [task(UUID_A)])), false);
  // Generation advance resets baseline.
  assert.equal(home.applySnapshot(snapshot("edge-a", 3, 1, [task(UUID_A)])), true);
});

await t("edge removal drops contribution", () => {
  const home = new HomePresence("home-1");
  home.applySnapshot(snapshot("edge-a", 1, 1, [task(UUID_A)]));
  home.applySnapshot(snapshot("edge-b", 1, 1, [task(UUID_B)]));
  assert.equal(home.removeEdge("edge-a"), true);
  assert.equal(home.mergedTasks().length, 1);
  assert.equal(home.mergedTasks()[0].taskPresenceId, UUID_B);
});

await t("ownerOf routes only to owning edge", () => {
  const home = new HomePresence("home-1");
  home.applySnapshot(snapshot("edge-a", 1, 1, [task(UUID_A)]));
  assert.equal(home.ownerOf(UUID_A), "edge-a");
  assert.equal(home.ownerOf(UUID_B), undefined);
});

await t("merged tasks preserve Task Presence ID and carry origin revision", () => {
  const home = new HomePresence("home-1");
  home.applySnapshot(snapshot("edge-a", 4, 41, [task(UUID_A)]));
  const merged = home.mergedTasks();
  assert.equal(merged[0].taskPresenceId, UUID_A);
  assert.equal(merged[0].originGeneration, 4);
  assert.equal(merged[0].originRevision, 41);
  // Feed projection strips origin metadata but preserves the ID.
  const feed = home.feedSnapshot();
  assert.equal(feed.tasks[0].taskPresenceId, UUID_A);
  assert.equal("originRevision" in feed.tasks[0], false);
});

await t("duplicate ids across edges converge by origin revision", () => {
  const home = new HomePresence("home-1");
  home.applySnapshot(snapshot("edge-a", 1, 3, [task(UUID_A, { state: "running" })]));
  home.applySnapshot(snapshot("edge-b", 1, 7, [task(UUID_A, { state: "ready", reason: "completed" })]));
  const merged = home.mergedTasks();
  assert.equal(merged.length, 1);
  assert.equal(merged[0].state, "ready"); // higher origin revision wins
});

await t("priority order and snooze exclusion in feed", () => {
  const home = new HomePresence("home-1");
  home.applySnapshot(snapshot("edge-a", 1, 1, [
    task(UUID_A, { state: "running" }),
    task(UUID_B, { state: "needs_input", reason: "permission" }),
  ]));
  const feed = home.feedSnapshot();
  assert.equal(feed.tasks[0].state, "needs_input");
  assert.equal(feed.aggregateState, "needs_input");
  assert.equal(feed.counts.needsInput, 1);
});

await t("upstream snapshot validation is fail-closed", () => {
  const good = snapshot("edge-a", 1, 1, [task(UUID_A)]);
  assert.ok(validateUpstreamSnapshot(good) !== null);
  // Unknown schema.
  assert.equal(validateUpstreamSnapshot({ ...good, schema: "agentagotchi.feed.v1" }), null);
  // Private extra field on a task.
  assert.equal(validateUpstreamSnapshot(snapshot("edge-a", 1, 1, [
    { ...task(UUID_A), nativeSessionId: "secret" },
  ])), null);
  // Private extra fields at top level also rejected via strict task check.
  assert.equal(validateFeedTask({ ...task(UUID_A), prompt: "tell me" }), null);
  // Bad UUID.
  assert.equal(validateFeedTask(task("not-a-uuid")), null);
  // Unknown capability.
  assert.equal(validateFeedTask(task(UUID_A, { capabilities: ["approve"] })), null);
});

await t("pairing ceremony: approve then redeem issues scoped credential", () => {
  const ceremony = new PairingCeremony();
  const code = ceremony.requestCode("feed", "BOX-3");
  assert.equal(ceremony.redeem(code.token), null); // unapproved grants nothing
  assert.equal(ceremony.approve(code.id), true);
  const cred = ceremony.redeem(code.token);
  assert.ok(cred !== null);
  assert.equal(cred.role, "feed");
  // One use.
  assert.equal(ceremony.redeem(code.token), null);
  // Authenticates; revocation blocks.
  assert.equal(ceremony.authenticate(cred.token)?.id, cred.id);
  ceremony.revoke(cred.id);
  assert.equal(ceremony.authenticate(cred.token), null);
});

await t("pairing: code expiry", () => {
  let now = Date.now();
  const ceremony = new PairingCeremony(() => now);
  const code = ceremony.requestCode("edge-ingress", "edge-a");
  now += 11 * 60 * 1000; // past TTL
  assert.equal(ceremony.approve(code.id), false);
});

await t("pairing: list redacts tokens", () => {
  const ceremony = new PairingCeremony();
  const code = ceremony.requestCode("feed", "BOX-3");
  ceremony.approve(code.id);
  const cred = ceremony.redeem(code.token);
  const listed = ceremony.list();
  assert.equal(listed[0].token, "");
  assert.notEqual(cred.token, "");
});

await t("pairing: persistence round trip", () => {
  const ceremony = new PairingCeremony();
  const code = ceremony.requestCode("feed", "BOX-3");
  ceremony.approve(code.id);
  const cred = ceremony.redeem(code.token);
  const restored = PairingCeremony.load(ceremony.dump());
  assert.equal(restored.authenticate(cred.token)?.id, cred.id);
});

console.log(`${passed}/12 tests passed`);
process.exit(process.exitCode ?? 0);
