/**
 * Shim hooks — bridge v1 view data shapes to v2 snapshot.
 *
 * When project === 'all', data is merged across all projects.
 * When project === a specific slug, data is scoped to that project.
 */

import type {
  Snapshot,
  TicketSummary,
  TicketDetail,
  TimelineEvent,
  AnalyticsRollup,
  ActiveSession,
} from './data';

const _emptyAnalytics: AnalyticsRollup = {
  rows: [],
  per_skill: [],
  per_day: [],
  outcome: [],
};

function mergeAnalytics(rollups: AnalyticsRollup[]): AnalyticsRollup {
  if (rollups.length === 0) return { ..._emptyAnalytics };
  if (rollups.length === 1) return rollups[0];

  const rows = rollups.flatMap(r => r.rows);

  // Merge per_skill
  const skillMap = new Map<string, { runs: number; total_s: number; success: number; error: number }>();
  for (const r of rollups) {
    for (const s of r.per_skill) {
      const existing = skillMap.get(s.skill) ?? { runs: 0, total_s: 0, success: 0, error: 0 };
      skillMap.set(s.skill, {
        runs: existing.runs + s.runs,
        total_s: existing.total_s + s.total_s,
        success: existing.success + s.success,
        error: existing.error + s.error,
      });
    }
  }
  const per_skill = Array.from(skillMap.entries()).map(([skill, v]) => ({ skill, ...v }));

  // Merge per_day
  const dayMap = new Map<string, number>();
  for (const r of rollups) {
    for (const d of r.per_day) {
      dayMap.set(d.day, (dayMap.get(d.day) ?? 0) + d.runs);
    }
  }
  const per_day = Array.from(dayMap.entries()).map(([day, runs]) => ({ day, runs })).sort((a, b) => a.day.localeCompare(b.day));

  // Merge outcome
  const outcomeMap = new Map<string, number>();
  for (const r of rollups) {
    for (const o of r.outcome) {
      outcomeMap.set(o.outcome, (outcomeMap.get(o.outcome) ?? 0) + o.count);
    }
  }
  const outcome = Array.from(outcomeMap.entries()).map(([outcome, count]) => ({ outcome, count }));

  return { rows, per_skill, per_day, outcome };
}

/** Scoped tickets — used by TicketsList + Home. */
export function useScopedTickets(snapshot: Snapshot, project: string): TicketSummary[] {
  if (project === 'all') {
    return Object.values(snapshot.projects).flatMap(p => p.tickets);
  }
  return snapshot.projects[project]?.tickets ?? [];
}

/** Scoped ticketDetail — used by TicketDetail. Cross-project search when project='all'. */
export function useScopedTicketDetail(
  snapshot: Snapshot,
  project: string,
  ticketId: string,
): TicketDetail | undefined {
  if (project !== 'all') {
    return snapshot.projects[project]?.ticketDetail[ticketId];
  }
  // Search across all projects
  for (const proj of Object.values(snapshot.projects)) {
    const detail = proj.ticketDetail[ticketId];
    if (detail) return detail;
  }
  return undefined;
}

/** Scoped timeline — used by Timeline. */
export function useScopedTimeline(snapshot: Snapshot, project: string): TimelineEvent[] {
  if (project === 'all') {
    return Object.values(snapshot.projects).flatMap(p => p.timeline);
  }
  return snapshot.projects[project]?.timeline ?? [];
}

/** Scoped analytics rollup — used by Analytics. */
export function useScopedAnalytics(snapshot: Snapshot, project: string): AnalyticsRollup {
  if (project === 'all') {
    return mergeAnalytics(Object.values(snapshot.projects).map(p => p.analytics));
  }
  return snapshot.projects[project]?.analytics ?? { ..._emptyAnalytics };
}

// v1.18+ snapshots populate SessionsInfo.sessions with structured rows. Pre-1.19
// snapshots only had {count, slugs} — return [] for those so callers degrade
// to the count-only render path.
export function useScopedSessions(snapshot: Snapshot): ActiveSession[] {
  return snapshot.sessions?.sessions ?? [];
}
