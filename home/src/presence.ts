// Presence merge: the Home's relay state. Per-Edge complete replacement
// (docs/adr/0004), origin generation/revision ordering, privacy-safe merged
// feed projection. Pure and unit-testable; the Durable Object drives it.

import type { Counts, FeedSnapshot, FeedTask, PresenceState, UpstreamSnapshot } from "./wire.ts";
import { SCHEMA_FEED } from "./wire.ts";

export interface EdgeContribution {
  edgeId: string;
  generation: number;
  revision: number;
  tasks: FeedTask[];
  connectedAt: string;
}

export interface MergedTask extends FeedTask {
  originEdgeId: string;
  originGeneration: number;
  originRevision: number;
}

const RANK: Record<string, number> = {
  needs_input: 4, blocked: 3, ready: 2, running: 1, idle: 0,
};

export class HomePresence {
  private edges = new Map<string, EdgeContribution>();
  private homeRevision = 0;
  private readonly homeId: string;

  constructor(homeId: string) {
    this.homeId = homeId;
  }

  /**
   * Replace one Edge's contribution with its newest absolute snapshot.
   * Stale or replayed snapshots (regressed generation/revision) are rejected;
   * a generation advance resets the revision baseline for that Edge.
   */
  applySnapshot(snapshot: UpstreamSnapshot): boolean {
    const existing = this.edges.get(snapshot.edgeId);
    if (existing !== undefined) {
      const regressed =
        snapshot.generation < existing.generation ||
        (snapshot.generation === existing.generation &&
          snapshot.revision <= existing.revision);
      if (regressed) return false;
    }
    this.edges.set(snapshot.edgeId, {
      edgeId: snapshot.edgeId,
      generation: snapshot.generation,
      revision: snapshot.revision,
      tasks: snapshot.tasks,
      connectedAt: snapshot.snapshotGeneratedAt,
    });
    this.homeRevision += 1;
    return true;
  }

  /** Drop an Edge's contribution when its connection ends. */
  removeEdge(edgeId: string): boolean {
    if (!this.edges.delete(edgeId)) return false;
    this.homeRevision += 1;
    return true;
  }

  /** The Task Presence's owning Edge, for reverse action routing. */
  ownerOf(taskPresenceId: string): string | undefined {
    for (const [edgeId, contribution] of this.edges) {
      for (const task of contribution.tasks) {
        if (task.taskPresenceId === taskPresenceId) return edgeId;
      }
    }
    return undefined;
  }

  /** Capability lookup for fail-closed action validation. */
  capabilitiesOf(taskPresenceId: string): string[] | undefined {
    for (const contribution of this.edges.values()) {
      for (const task of contribution.tasks) {
        if (task.taskPresenceId === taskPresenceId) return task.capabilities;
      }
    }
    return undefined;
  }

  revision(): number {
    return this.homeRevision;
  }

  edgeIds(): string[] {
    return [...this.edges.keys()];
  }

  /**
   * Merge all Edge contributions into the device-facing snapshot. Task
   * Presence IDs are preserved unchanged and each merged task carries its
   * origin generation/revision so a duplicate direct/relayed copy converges
   * on the device to the newest origin value.
   */
  mergedTasks(): MergedTask[] {
    const byId = new Map<string, MergedTask>();
    for (const contribution of this.edges.values()) {
      for (const task of contribution.tasks) {
        const merged: MergedTask = {
          ...task,
          originEdgeId: contribution.edgeId,
          originGeneration: contribution.generation,
          originRevision: contribution.revision,
        };
        const existing = byId.get(task.taskPresenceId);
        if (
          existing === undefined ||
          merged.originGeneration > existing.originGeneration ||
          (merged.originGeneration === existing.originGeneration &&
            merged.originRevision > existing.originRevision)
        ) {
          byId.set(task.taskPresenceId, merged);
        }
      }
    }
    const tasks = [...byId.values()];
    tasks.sort((a, b) => {
      if (a.snoozed !== b.snoozed) return a.snoozed ? 1 : -1;
      const rankDelta = (RANK[b.state] ?? 0) - (RANK[a.state] ?? 0);
      if (rankDelta !== 0) return rankDelta;
      return b.updatedAt.localeCompare(a.updatedAt);
    });
    return tasks;
  }

  /** Project the privacy-safe feed snapshot for devices. */
  feedSnapshot(): FeedSnapshot {
    const merged = this.mergedTasks();
    const counts: Counts = { needsInput: 0, blocked: 0, ready: 0, running: 0 };
    for (const task of merged) {
      if (task.state === "needs_input") counts.needsInput += 1;
      else if (task.state === "blocked") counts.blocked += 1;
      else if (task.state === "ready") counts.ready += 1;
      else if (task.state === "running") counts.running += 1;
    }
    const aggregate: PresenceState = merged.length > 0 ? merged[0].state : "idle";
    return {
      schema: SCHEMA_FEED,
      type: "snapshot",
      origin: {
        kind: "home",
        id: this.homeId,
        generation: 1,
        revision: this.homeRevision,
      },
      generatedAt: new Date().toISOString(),
      aggregateState: aggregate,
      counts,
      // Devices receive the allowlisted projection; origin metadata is
      // carried in the task entries' ordering, not as extra private fields.
      tasks: merged.map((task) => ({
        taskPresenceId: task.taskPresenceId,
        safeTitle: task.safeTitle,
        state: task.state,
        reason: task.reason,
        subagentCount: task.subagentCount,
        capabilities: task.capabilities,
        updatedAt: task.updatedAt,
        snoozed: task.snoozed,
      })),
    };
  }
}
