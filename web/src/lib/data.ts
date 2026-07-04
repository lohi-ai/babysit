// Snapshot schema — single source of truth.
// `bin/bbs-dashboard` writes `web/dist/data.js` conforming to `Snapshot`,
// loaded via `<script src="./data.js">` into `window.__BBS_DATA__`.

export type TicketStatus =
  | 'triage' | 'backlog' | 'planned' | 'decomposed'
  | 'in_progress' | 'in_review' | 'blocked'
  | 'done' | 'cancelled' | 'duplicate'
  | 'unknown';

// v2 meta — replaces v1 Meta
export interface Meta {
  schema_version: 2;
  generated_at: string;
  babysit_version: string;
  active_project: string | null;
  truncations: TruncationMarker[];
  // v1 compat fields (may be absent on v2 snapshots)
  snapshot_at?: string;
  slug?: string;
  active_pair?: { ticket: string; workflow: string; step: string; branch: string } | null;
  _stale?: boolean;
}

export interface TruncationMarker {
  kind: 'decisions' | 'skillEvents' | 'tickets';
  kept: number;
  total: number;
  forced?: boolean;
}

export interface TicketSummary {
  id: string;
  title: string;
  status: TicketStatus;
  phase: string | null;
  branch: string | null;
  parent: string | null;
  size: string | null;
  updated_at: string | null;
  created_at: string | null;
}

export interface NamedFile {
  name: string;
  body: string;
}

export interface CheckpointRow {
  ticket: string;
  workflow: string;
  step: string;
  status: string;
  note: string;
  branch: string;
  head_sha?: string;
  updated_at: string;
}

export interface HistoryRow {
  ts: string;
  event: string;
  workflow?: string;
  step?: string;
  status?: string;
  note?: string;
  actor?: string;
  branch?: string;
  [k: string]: unknown;
}

// Repo row from manifest.yaml — the v1.18 identity anchor. One per repo
// the ticket touches (single-repo mode emits one entry; product-mode emits
// one per declared repository).
export interface ManifestRepo {
  name: string | null;
  branch: string | null;
  canonical: string | null;
  worktree: string | null;
  base: string | null;
  pushed: boolean;
}

export interface TicketDetail extends TicketSummary {
  requirement: string | null;
  plan: string | null;
  manifest: string | null;
  repos: ManifestRepo[];
  checkpoint: CheckpointRow | null;
  history: HistoryRow[];
  handoffs: NamedFile[];
  verdicts: NamedFile[];
  // Per-skill categorical status parsed from verdicts/<skill>.md — same
  // alphabet as `bbs-ticket verdict-status`:
  // none | DONE | DONE_WITH_CONCERNS | BLOCKED | NEEDS_CONTEXT
  verdict_statuses: Record<string, string>;
  reviews: NamedFile[];
  evidence: string[];
}

export interface TimelineEvent {
  ts: string;
  ticket: string;
  workflow?: string;
  step?: string;
  status?: string;
  note?: string;
  event?: string;
  actor?: string;
  branch?: string;
  [k: string]: unknown;
}

export interface SkillUsageRow {
  ts: string;
  skill: string;
  event: string;
  session?: string;
  duration_s?: number;
  outcome?: string;
  [k: string]: unknown;
}

export interface AnalyticsRollup {
  rows: SkillUsageRow[];
  per_skill: { skill: string; runs: number; total_s: number; success: number; error: number }[];
  per_day: { day: string; runs: number }[];
  outcome: { outcome: string; count: number }[];
}

// Per-project data block under projects[slug]
export interface ProjectBlock {
  tickets: TicketSummary[];
  ticketDetail: Record<string, TicketDetail>;
  timeline: TimelineEvent[];
  analytics: AnalyticsRollup;
}

// v2 new row types
export interface DecisionRow {
  ts: string;
  skill: string;
  phase: string;
  classification: string;
  principle: string;
  decision: string;
  context?: string;
  [k: string]: unknown;
}

export interface SkillEventRow {
  ts: string;
  skill: string;
  event: 'start' | 'end' | string;
  session?: string;
  duration_s?: number;
  outcome?: string;
  [k: string]: unknown;
}

export interface BuilderRow {
  ts: string;
  date: string;
  signals?: Record<string, unknown>;
  assignment?: string;
  topics?: string[];
  mood?: string;
  [k: string]: unknown;
}

// Active session — one per live ~/.babysit/sessions/<id>.yaml file (mtime
// within the last 120 minutes). `ticket` / `product` / `cwd` come from
// parsing the yaml body; missing fields are null when the file was
// half-written or pre-1.18.
export interface ActiveSession {
  id: string;
  ticket: string | null;
  product: string | null;
  cwd: string | null;
  started_at: string | null;
  age_min: number;
}

// Sessions block — count is the live total (mtime ≤ 120m). `slugs` and
// `sessions` are kept in parallel: `slugs` for v1.18 back-compat, `sessions`
// for the structured render path. Order: freshest first (smallest age_min).
export interface SessionsInfo {
  count: number;
  slugs?: string[];
  sessions?: ActiveSession[];
}

export interface Snapshot {
  meta: Meta;
  // v2: per-project data keyed by slug
  projects: Record<string, ProjectBlock>;
  // v2: global data sources
  decisions: DecisionRow[];
  skillEvents: SkillEventRow[];
  builderProfile: BuilderRow[];
  journalTail: string[];
  sessions: SessionsInfo;
  // v1 compat fields (present on v1 snapshots, absent on v2)
  tickets?: TicketSummary[];
  ticketDetail?: Record<string, TicketDetail>;
  timeline?: TimelineEvent[];
  analytics?: AnalyticsRollup;
}

// _stale is set on the loaded snapshot when schema_version < 2
export interface LoadedSnapshot extends Snapshot {
  _stale: boolean;
}

declare global {
  interface Window { __BBS_DATA__?: Snapshot }
}

const _emptyAnalytics: AnalyticsRollup = { rows: [], per_skill: [], per_day: [], outcome: [] };

export function loadSnapshot(): LoadedSnapshot {
  if (typeof window === 'undefined' || !window.__BBS_DATA__) {
    throw new Error('data.js not loaded — run `bbs-dashboard build` then re-snapshot');
  }
  const s = window.__BBS_DATA__ as LoadedSnapshot;

  // Runtime schema check: if not v2, mark stale and apply defensive defaults.
  const version = (s.meta as { schema_version?: number })?.schema_version ?? 1;
  if (version < 2) {
    s.meta = { ...(s.meta as object) as Meta, schema_version: 2, generated_at: (s.meta as { snapshot_at?: string })?.snapshot_at ?? '', active_project: (s.meta as { slug?: string })?.slug ?? null, truncations: [], _stale: true };
    s._stale = true;
  } else {
    s._stale = false;
  }

  // Defensive defaults for v2 fields
  s.projects ??= {};
  for (const slug of Object.keys(s.projects)) {
    const p = s.projects[slug];
    p.tickets ??= [];
    p.ticketDetail ??= {};
    p.timeline ??= [];
    p.analytics ??= { ..._emptyAnalytics };
    for (const id of Object.keys(p.ticketDetail)) {
      const d = p.ticketDetail[id];
      d.history ??= [];
      d.handoffs ??= [];
      d.verdicts ??= [];
      d.verdict_statuses ??= {};
      d.reviews ??= [];
      d.evidence ??= [];
      d.repos ??= [];
    }
  }
  s.decisions ??= [];
  s.skillEvents ??= [];
  s.builderProfile ??= [];
  s.journalTail ??= [];
  s.sessions ??= { count: 0, slugs: [], sessions: [] };
  if (typeof s.sessions === 'object' && !('count' in s.sessions)) {
    s.sessions = { count: (s.sessions as unknown as ActiveSession[]).length ?? 0, sessions: [] };
  }
  s.sessions.sessions ??= [];
  s.meta.truncations ??= [];

  // v1 compat: if top-level tickets/timeline/analytics present (v1 shape),
  // migrate them into projects[slug] for v1 SPA components that might read them.
  // (v2 SPA reads from s.projects[slug]; this is a best-effort shim for stale files.)
  if (s._stale && (s.tickets || s.timeline || s.analytics)) {
    const slug = (s.meta as { slug?: string })?.slug ?? '__v1__';
    if (!s.projects[slug]) {
      s.projects[slug] = {
        tickets: s.tickets ?? [],
        ticketDetail: s.ticketDetail ?? {},
        timeline: s.timeline ?? [],
        analytics: s.analytics ?? { ..._emptyAnalytics },
      };
    }
  }

  return s;
}
