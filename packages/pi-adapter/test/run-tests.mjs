// Plain-script test harness for EdgePresenceClient (node --test hangs on
// top-level await imports in this repo's Node version; run directly).
import assert from "node:assert/strict";
import { createServer } from "node:net";
import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

const dir = mkdtempSync(join(tmpdir(), "agot-test-"));
process.env.AGENTAGOTCHI_EDGE_SOCKET = join(dir, "edge.sock");
const { EdgePresenceClient } = await import("../edge-ipc.ts");

class FakeEdge {
  constructor(path) {
    this.path = path;
    this.frames = [];
    this.sockets = [];
    this.leaseCounter = 0;
    this.server = createServer((socket) => {
      this.sockets.push(socket);
      let buffer = "";
      socket.on("data", (chunk) => {
        buffer += chunk.toString();
        let idx;
        while ((idx = buffer.indexOf("\n")) >= 0) {
          const line = buffer.slice(0, idx);
          buffer = buffer.slice(idx + 1);
          const frame = JSON.parse(line);
          this.frames.push(frame);
          if (frame.type === "adapter_hello") {
            this.leaseCounter += 1;
            socket.write(JSON.stringify({
              schema: "agentagotchi.ipc.v1",
              type: "hello_ack",
              leaseId: `lease-${this.leaseCounter}`,
              leaseSeconds: 30,
            }) + "\n");
          }
        }
      });
    });
  }
  listen() { return new Promise((r) => this.server.listen(this.path, r)); }
  closeAllSockets() { for (const s of this.sockets) s.destroy(); this.sockets = []; }
  async close() { this.closeAllSockets(); await new Promise((r) => this.server.close(r)); }
}

const wait = (ms) => new Promise((r) => setTimeout(r, ms));
async function waitFor(fn, ms = 4000) {
  const deadline = Date.now() + ms;
  for (;;) {
    const v = fn();
    if (v !== undefined) return v;
    if (Date.now() > deadline) throw new Error("waitFor timed out");
    await wait(10);
  }
}
const report = (id, state, reason) => ({
  nativeSessionId: id, safeTitle: "Pi", state, reason, subagentCount: 0,
});

let passed = 0;
async function t(name, fn) {
  try { await fn(); passed++; console.log("ok -", name); }
  catch (err) { console.error("FAIL -", name, err); process.exitCode = 1; }
}

await t("leased session reports absolute presence", async () => {
  const edge = new FakeEdge(process.env.AGENTAGOTCHI_EDGE_SOCKET);
  await edge.listen();
  const client = new EdgePresenceClient();
  await client.report(report("sess-1", "running", "working"));
  const hello = await waitFor(() => edge.frames.find((f) => f.type === "adapter_hello"));
  assert.equal(hello.schema, "agentagotchi.ipc.v1");
  assert.equal(hello.harness, "pi");
  assert.deepEqual(hello.capabilities, []); // status-only: no Focus
  const rep = await waitFor(() => edge.frames.find((f) => f.type === "presence_report"));
  assert.equal(rep.leaseId, "lease-1");
  assert.equal(rep.producerSeq, 1);
  assert.equal(rep.reports[0].nativeSessionId, "sess-1");
  assert.equal(rep.reports[0].state, "running");
  // Exactly one report (no double-send after fresh connect).
  await wait(100);
  assert.equal(edge.frames.filter((f) => f.type === "presence_report").length, 1);
  await client.close();
  await edge.close();
});

await t("producer sequence increments monotonically", async () => {
  const edge = new FakeEdge(process.env.AGENTAGOTCHI_EDGE_SOCKET);
  await edge.listen();
  const client = new EdgePresenceClient();
  await client.report(report("sess-1", "running", "working"));
  await waitFor(() => edge.frames.find((f) => f.type === "presence_report"));
  await client.report(report("sess-1", "ready", "completed"));
  await waitFor(() => edge.frames.filter((f) => f.type === "presence_report").length >= 2 || undefined);
  const seqs = edge.frames.filter((f) => f.type === "presence_report").map((f) => f.producerSeq);
  assert.deepEqual([...seqs].sort((a, b) => a - b), [1, 2]);
  await client.close();
  await edge.close();
});

await t("reconnect resends complete absolute state", async () => {
  const edge = new FakeEdge(process.env.AGENTAGOTCHI_EDGE_SOCKET);
  await edge.listen();
  const client = new EdgePresenceClient();
  await client.report(report("sess-1", "running", "working"));
  await waitFor(() => edge.frames.find((f) => f.type === "presence_report"));
  edge.closeAllSockets();
  await waitFor(() => edge.frames.filter((f) => f.type === "adapter_hello").length >= 2 || undefined, 8000);
  const resend = await waitFor(() =>
    edge.frames.find((f) => f.type === "presence_report" && f.leaseId === "lease-2"));
  assert.equal(resend.reports[0].nativeSessionId, "sess-1");
  assert.equal(resend.reports[0].state, "running");
  await client.close();
  await edge.close();
});

await t("end removes presence from absolute state", async () => {
  const edge = new FakeEdge(process.env.AGENTAGOTCHI_EDGE_SOCKET);
  await edge.listen();
  const client = new EdgePresenceClient();
  await client.report(report("sess-1", "ready", "completed"));
  await waitFor(() => edge.frames.find((f) => f.type === "presence_report"));
  await client.end("sess-1");
  const endFrame = await waitFor(() =>
    edge.frames.find((f) => f.type === "presence_report" && (f.ends || []).includes("sess-1")));
  assert.deepEqual(endFrame.ends, ["sess-1"]);
  edge.closeAllSockets();
  await wait(6000); // longer than RECONNECT_MS
  const resends = edge.frames.filter((f) => f.type === "presence_report" && f.leaseId === "lease-2");
  assert.equal(resends.length, 0);
  await client.close();
  await edge.close();
});

await t("offline edge retains state for later resend", async () => {
  const client = new EdgePresenceClient();
  await client.report(report("sess-offline", "running", "working")); // no server: must not throw
  const edge = new FakeEdge(process.env.AGENTAGOTCHI_EDGE_SOCKET);
  await edge.listen();
  await client.report(report("sess-offline", "ready", "completed"));
  const rep = await waitFor(() => edge.frames.find((f) => f.type === "presence_report"));
  assert.equal(rep.reports[0].state, "ready");
  await client.close();
  await edge.close();
});

rmSync(dir, { recursive: true, force: true });
console.log(`${passed}/5 tests passed`);
process.exit(process.exitCode ?? 0);
