// Edge IPC client for the Pi Harness Adapter.
//
// Speaks agentagotchi.ipc.v1 over the owner-only Unix domain socket as a
// leased adapter session (docs/PROTOCOL.md): adapter_hello, heartbeat,
// absolute presence_report, ends on shutdown. Frames are newline-delimited
// JSON bounded at 64 KiB; reconnect resends absolute current state.

import { connect, type Socket } from "node:net";
import { homedir, platform } from "node:os";
import { join } from "node:path";

export type PresenceState = "idle" | "running" | "needs_input" | "ready" | "blocked";

export interface PresenceReport {
	nativeSessionId: string;
	safeTitle: string;
	state: PresenceState;
	reason: string;
	subagentCount: number;
}

const SCHEMA = "agentagotchi.ipc.v1";
const MAX_FRAME = 64 * 1024;
const RECONNECT_MS = 5000;

function defaultSocketPath(): string {
	if (process.env.AGENTAGOTCHI_EDGE_SOCKET) return process.env.AGENTAGOTCHI_EDGE_SOCKET;
	const home = homedir();
	if (platform() === "darwin") {
		return join(home, "Library", "Application Support", "Agentagotchi", "edge.sock");
	}
	return join(home, ".local", "share", "agentagotchi", "edge.sock");
}

export class EdgePresenceClient {
	private socket: Socket | undefined;
	private leaseId: string | undefined;
	private leaseSeconds = 30;
	private heartbeatTimer: NodeJS.Timeout | undefined;
	private reconnectTimer: NodeJS.Timeout | undefined;
	private producerSeq = 0;
	private buffer = "";
	private readonly absolute = new Map<string, PresenceReport>();
	private connecting: Promise<void> | undefined;

	async connect(): Promise<void> {
		if (this.socket && this.leaseId) return;
		this.connecting ??= this.dial().finally(() => {
			this.connecting = undefined;
		});
		return this.connecting;
	}

	private dial(): Promise<void> {
		return new Promise((resolve, reject) => {
			const socket = connect(defaultSocketPath());
			let settled = false;
			socket.once("error", (err) => {
				if (!settled) {
					settled = true;
					reject(err);
				}
				this.handleDrop(socket);
			});
			socket.once("close", () => this.handleDrop(socket));
			socket.on("data", (chunk) => this.onData(chunk));
			socket.on("connect", () => {
				this.socket = socket;
				this.send({
					schema: SCHEMA,
					type: "adapter_hello",
					harness: "pi",
					adapterVersion: "0.1.0",
					capabilities: [], // Status-only: no Focus, by design.
				});
				this.awaitHelloAck()
					.then(() => {
						if (!settled) {
							settled = true;
							resolve();
						}
					})
					.catch((err) => {
						if (!settled) {
							settled = true;
							reject(err);
						}
					});
			});
		});
	}

	private pendingAck: ((frame: Record<string, unknown>) => void) | undefined;

	private awaitHelloAck(): Promise<void> {
		return new Promise((resolve, reject) => {
			const timeout = setTimeout(() => reject(new Error("hello_ack timeout")), 5000);
			this.pendingAck = (frame) => {
				clearTimeout(timeout);
				if (frame.type !== "hello_ack" || typeof frame.leaseId !== "string") {
					reject(new Error("unexpected frame before hello_ack"));
					return;
				}
				this.leaseId = frame.leaseId;
				if (typeof frame.leaseSeconds === "number" && frame.leaseSeconds > 0) {
					this.leaseSeconds = frame.leaseSeconds;
				}
				this.startHeartbeat();
				this.resendAbsolute();
				resolve();
			};
		});
	}

	private onData(chunk: Buffer) {
		this.buffer += chunk.toString("utf8");
		if (this.buffer.length > MAX_FRAME) {
			this.socket?.destroy();
			return;
		}
		let newline: number;
		while ((newline = this.buffer.indexOf("\n")) >= 0) {
			const line = this.buffer.slice(0, newline);
			this.buffer = this.buffer.slice(newline + 1);
			if (!line.trim()) continue;
			let frame: Record<string, unknown>;
			try {
				frame = JSON.parse(line);
			} catch {
				continue;
			}
			if (frame.schema !== SCHEMA) continue;
			if (this.pendingAck) {
				const ack = this.pendingAck;
				this.pendingAck = undefined;
				ack(frame);
			}
			// action_request frames are ignored: Pi advertises no capabilities.
		}
	}

	private startHeartbeat() {
		this.stopHeartbeat();
		const interval = Math.max(1000, (this.leaseSeconds / 2) * 1000);
		this.heartbeatTimer = setInterval(() => {
			if (this.leaseId) {
				this.send({ schema: SCHEMA, type: "heartbeat", leaseId: this.leaseId });
			}
		}, interval);
		this.heartbeatTimer.unref();
	}

	private stopHeartbeat() {
		if (this.heartbeatTimer) clearInterval(this.heartbeatTimer);
		this.heartbeatTimer = undefined;
	}

	private handleDrop(dropped: Socket) {
		// A stale socket's late close/error must not clear state owned by a
		// newer connection (the old socket's close can fire after the new
		// socket has connected).
		if (this.socket === dropped) {
			this.socket = undefined;
			this.leaseId = undefined;
			this.stopHeartbeat();
		}
		if (this.absolute.size > 0 && !this.reconnectTimer) {
			this.reconnectTimer = setTimeout(() => {
				this.reconnectTimer = undefined;
				void this.connect().catch(() => {});
			}, RECONNECT_MS);
			this.reconnectTimer.unref();
		}
	}

	/** Resend complete absolute state so the Edge converges after reconnect. */
	private resendAbsolute() {
		if (this.absolute.size === 0) return;
		this.sendReport([...this.absolute.values()], []);
	}

	async report(report: PresenceReport): Promise<void> {
		this.absolute.set(report.nativeSessionId, report);
		const wasConnected = Boolean(this.socket && this.leaseId);
		try {
			await this.connect();
		} catch {
			return; // Edge offline: absolute state is retained for resend.
		}
		// A fresh connect already resent the absolute map including this
		// report; only send immediately when the session was already live.
		if (wasConnected) {
			this.sendReport([report], []);
		}
	}

	async end(nativeSessionId: string): Promise<void> {
		this.absolute.delete(nativeSessionId);
		if (!this.leaseId) return;
		this.sendReport([], [nativeSessionId]);
	}

	private sendReport(reports: PresenceReport[], ends: string[]) {
		if (!this.leaseId) return;
		this.producerSeq += 1;
		this.send({
			schema: SCHEMA,
			type: "presence_report",
			leaseId: this.leaseId,
			producerSeq: this.producerSeq,
			reports: reports.map((r) => ({
				nativeSessionId: r.nativeSessionId,
				displayKey: "",
				safeTitle: r.safeTitle,
				state: r.state,
				reason: r.reason,
				subagentCount: r.subagentCount,
			})),
			ends,
		});
	}

	private send(frame: Record<string, unknown>) {
		const line = JSON.stringify(frame);
		if (line.length > MAX_FRAME || !this.socket) return;
		this.socket.write(line + "\n");
	}

	async close(): Promise<void> {
		this.stopHeartbeat();
		if (this.reconnectTimer) clearTimeout(this.reconnectTimer);
		this.reconnectTimer = undefined;
		this.absolute.clear();
		this.socket?.destroy();
		this.socket = undefined;
		this.leaseId = undefined;
	}
}
