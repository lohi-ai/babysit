// Snapshot schema — single source of truth for BOTH data sources.
//
// The same `Snapshot` object arrives two ways, and nothing downstream of
// `loadSnapshot` / `fetchSnapshot` can tell which:
//
//   snapshot mode — `bbs dashboard --snapshot` writes `web/dist/data.js` and
//     the page is opened over file://. `<script src="./data.js">` sets
//     `window.__BBS_DATA__` before the SPA boots. Read-only: there is no
//     server to POST to, so mutation controls render disabled.
//   served mode — `bbs dashboard` runs a localhost server that composes the
//     very same object and serves it at `GET /api/snapshot`. Its `/data.js`
//     is deliberately empty, which is what leaves `window.__BBS_DATA__`
//     undefined and selects this branch.

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
  // The repo folder the dashboard server was launched from. Prefilled as the
  // workspace folder when spawning a foreman. Absent on pre-1.63 snapshots,
  // empty when the server was launched outside a git repo.
  current_dir?: string;
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

// A human's override of the lifecycle, orthogonal to `status`. `prior_status`
// is the rung the ticket was on when the override landed, which is what makes
// resume/restore exact rather than a guess — so a control action never writes
// `status`, and `status` here is never the string "paused".
export interface TicketControl {
  state: 'paused' | 'cancelled';
  prior_status: TicketStatus | string;
  note: string;
  actor: string;
  at: string;
}

// The design checkpoint, published by a worker and answered here. `state` is
// the whole lifecycle: `pending` is a question waiting on a human, everything
// else is the answer it got. `resolved` is absent while pending.
// One comment anchored to a paragraph of a document or an element of the
// prototype. `anchor` locates it in the current render; `excerpt` is what the
// human pointed at, and is what still means something after a rewrite.
export interface ApprovalComment {
  id: string;
  target: 'requirement' | 'plan' | 'design' | 'prototype';
  anchor?: string;
  excerpt?: string;
  body: string;
  actor: string;
  at: string;
}

export interface TicketApproval {
  state: 'pending' | 'approved' | 'redirected' | 'dropped';
  kind: string;
  note: string;
  requested_by: string;
  at: string;
  comments?: ApprovalComment[];
  resolved?: {
    outcome: 'approved' | 'redirected' | 'dropped';
    actor: string;
    at: string;
    note: string;
  };
}

// The mock the approval is about. `html` is null when the file was too big to
// embed — the frame then has nothing to show and the tab link is the only way
// in, which the UI says out loud rather than rendering a blank box.
export interface TicketPrototype {
  path: string;
  bytes: number;
  html: string | null;
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
  /** Foreman id this ticket is assigned to; null when unassigned. */
  assignee: string | null;
  control: TicketControl | null;
  /** Pending or last-answered design checkpoint; null when never published. */
  approval: TicketApproval | null;
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
  design: string | null;
  prototype: TicketPrototype | null;
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

// One ~/.babysit/foremen/<id>.yaml record. `assigned` is derived server-side
// from the tickets; liveness is NOT — the raw `heartbeat` stamp is graded at
// render time, since a snapshot that baked "live" in would keep claiming it
// long after the foreman died.
export interface ForemanRow {
  id: string;
  owner: string;
  project_dir: string;
  workspace_dir: string;
  workspace_ref: string;
  workspace_title: string;
  session: string;
  status: string;
  heartbeat: string;
  /** RFC3339 stamp of the last poke that could not be delivered; '' when reachable. */
  unreachable: string;
  assigned: number;
}

/** Mirrors internal/foreman.StaleAfter — no heartbeat within this window reads as dead. */
export const FOREMAN_STALE_MS = 10 * 60 * 1000;

export function foremanLive(f: ForemanRow, now = Date.now()): boolean {
  const t = Date.parse(f.heartbeat ?? '');
  return Number.isFinite(t) && now - t < FOREMAN_STALE_MS;
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
  foremen: ForemanRow[];
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

/** Which of the two sources this page is running against. */
export type DataSource = 'snapshot' | 'server';

/**
 * `window.__BBS_DATA__` is the switch. The served page ships an empty
 * `/data.js`, so its absence means "there is a server behind this page" —
 * which is also what makes the mutation controls available.
 */
export function dataSource(): DataSource {
  return typeof window !== 'undefined' && window.__BBS_DATA__ ? 'snapshot' : 'server';
}

export function loadSnapshot(): LoadedSnapshot {
  if (typeof window === 'undefined' || !window.__BBS_DATA__) {
    throw new Error('data.js not loaded — run `bbs-dashboard build` then re-snapshot');
  }
  return normalizeSnapshot(window.__BBS_DATA__);
}

/** Served mode: the same object, over HTTP. */
export async function fetchSnapshot(signal?: AbortSignal): Promise<LoadedSnapshot> {
  const res = await fetch('/api/snapshot', { signal, headers: { Accept: 'application/json' } });
  if (!res.ok) {
    throw new Error(`GET /api/snapshot failed: ${res.status} ${res.statusText}`);
  }
  return normalizeSnapshot(await res.json());
}

/**
 * The defensive-defaults pass both sources share. It mutates and returns its
 * argument — the snapshot path has always handed views the same object
 * `window.__BBS_DATA__` points at, and copying it here would only double the
 * memory of a multi-megabyte snapshot.
 */
export function normalizeSnapshot(raw: unknown): LoadedSnapshot {
  const s = raw as LoadedSnapshot;

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
    // A data.js written before the control plane shipped has neither key; the
    // views read them unconditionally, so default here rather than at every
    // call site.
    for (const t of p.tickets) {
      t.assignee ??= null;
      t.control ??= null;
      t.approval ??= null;
    }
    for (const id of Object.keys(p.ticketDetail)) {
      const d = p.ticketDetail[id];
      d.history ??= [];
      d.handoffs ??= [];
      d.verdicts ??= [];
      d.verdict_statuses ??= {};
      d.reviews ??= [];
      d.evidence ??= [];
      d.repos ??= [];
      d.assignee ??= null;
      d.control ??= null;
      d.approval ??= null;
      d.design ??= null;
      d.prototype ??= null;
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
  s.foremen ??= [];
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
