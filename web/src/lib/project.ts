export interface ProjectEvidence {
  state: string;
  problems: string[];
  id: string;
  kind: string;
  subject: string;
  producer: string;
  finished: string;
  surfaces: { dir: string; ref: string; head: string }[];
  checks: { criterion: string; kind: string; exit_code: number | null; log: string }[];
  findings: { severity: string; summary: string; criterion: string }[];
  preview?: string;
  runtime?: string;
}

export interface ProjectSnapshot {
  ticket: string;
  observed_at: string;
  subject: string;
  contract: {
    audience: string;
    outcome: string;
    non_goals: string[];
    first_journey: string[];
    deadline?: string;
  } | null;
  approval: string;
  ready: boolean;
  completion: string;
  coordinator: { id?: string; heartbeat?: string; runtime_observed_at?: string; agent_liveness: string };
  dispatch_blockers: string[];
  blockers: string[];
  coverage: { id: string; text: string; owner: string; checks: string[]; state: string; evidence: string[] }[];
  children: {
    id: string;
    title: string;
    seed: string;
    state: string;
    blocked_by: string[];
    problems: string[];
    repos: { branch: string; head: string; finish: string }[];
    progress?: {
      phase: string; summary: string; health: string; updated_at: string;
      progress_at: string; wait_kind: string; wait_reason: string; wait_until: string;
      dispatch_id: string; attempt_id: string;
    };
  }[];
  evidence: ProjectEvidence[];
  delivery: { ticket: string; policy: string; state: string; head: string; url?: string; reason?: string }[];
  provider_usage: { available: boolean; reason?: string };
}

export async function readProject(project: string, ticket: string, signal: AbortSignal): Promise<ProjectSnapshot> {
  const response = await fetch(`/api/tickets/${encodeURIComponent(project)}/${encodeURIComponent(ticket)}/project`, { signal });
  const body = await response.json();
  if (!response.ok) throw new Error(body.error || `Project state unavailable (${response.status})`);
  return body;
}

export function projectLink(value: string | undefined): string | undefined {
  if (!value) return undefined;
  try {
    const url = new URL(value);
    if ((url.protocol === 'http:' || url.protocol === 'https:') && !url.username && !url.password) return url.href;
  } catch { /* Untrusted local state is rendered as text, never a URL. */ }
  return undefined;
}
