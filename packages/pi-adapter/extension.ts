// Agentagotchi Pi Harness Adapter (status-only).
//
// A Pi extension that reports honest session presence to the local Edge
// Bridge over the owner-only IPC socket. It maps only explicit Pi signals:
//
//   agent_start / session active -> running + working
//   agent_settled with ctx.isIdle() -> ready + completed
//   session_shutdown / unload    -> end (lease expiry also ends presences)
//
// It never synthesizes needs_input from tool names or UI heuristics, never
// advertises a Focus capability, and never transmits prompts, transcripts,
// tool input, session file paths, full cwd, or prompt-derived session names.
// Pi's stable session ID is used only inside the Edge-private mapping.
//
// Configuration (environment):
//   AGENTAGOTCHI_EDGE_SOCKET - path to edge.sock (default: platform data dir)

import type { ExtensionAPI, ExtensionContext } from "@earendil-works/pi-coding-agent";
import { EdgePresenceClient, type PresenceState } from "./edge-ipc.js";

const MAX_SUBAGENTS = 0; // Pi subagent counting is not yet observable here.

export default function agentagotchiAdapter(pi: ExtensionAPI) {
	const client = new EdgePresenceClient();
	let sessionId: string | undefined;
	let current: PresenceState | undefined;
	let stopped = false;

	async function report(state: PresenceState, reason: string) {
		if (stopped || !sessionId) return;
		current = state;
		await client.report({
			nativeSessionId: sessionId,
			safeTitle: "Pi",
			state,
			reason,
			subagentCount: MAX_SUBAGENTS,
		});
	}

	pi.on("session_start", async (_event, ctx) => {
		sessionId = ctx.sessionManager.getSessionId();
		if (!sessionId) return; // Ephemeral sessions have no stable identity.
		await client.connect();
		// Absolute resend on (re)start: settle state converges the Edge.
		if (current) {
			await report(current, "working");
		}
	});

	pi.on("agent_start", async () => {
		await report("running", "working");
	});

	pi.on("agent_settled", async (_event, ctx: ExtensionContext) => {
		// Explicit idle confirmation only: agent_settled guarantees Pi will
		// not continue automatically. ctx.isIdle() is true unless another
		// extension started a new run.
		if (ctx.isIdle()) {
			await report("ready", "completed");
		}
	});

	pi.on("session_shutdown", async () => {
		if (sessionId) {
			await client.end(sessionId);
		}
		stopped = true;
		await client.close();
	});
}
