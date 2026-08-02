// The mutation half of the control plane. Reads live in data.ts; these are the
// POSTs the dashboard makes on the human's behalf, all of them against the
// localhost server that served the page.
//
// Over file:// there is no server, so callers must gate on
// `dataSource() === 'server'` and render controls disabled rather than let a
// click fail — see data.ts.

export interface ApiError extends Error {
  status: number;
}

async function post<T>(path: string, body: unknown): Promise<T> {
  const res = await fetch(path, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body ?? {}),
  });
  // The server answers every failure with {"error": "..."} — a plain status
  // code tells the human nothing about which precondition they missed.
  const payload = await res.json().catch(() => ({}));
  if (!res.ok) {
    const err = new Error(payload.error ?? `${res.status} ${res.statusText}`) as ApiError;
    err.status = res.status;
    throw err;
  }
  return payload as T;
}

export interface CreateTicketResult {
  ticket: string;
  home: string;
  requirement: string;
}

/** `title` defaults to the requirement's first line. */
export function createTicket(project: string, requirement: string, title?: string) {
  return post<CreateTicketResult>('/api/tickets', {
    project,
    requirement,
    title: title ?? '',
  });
}

export interface AssignResult {
  ticket: string;
  assignee: string;
  /** How the wake went: sent | unreachable | cmux-unavailable | unknown-foreman.
   *  Absent when the call unassigned. Never an error — the assignment is on
   *  disk either way and the foreman picks it up on its next tick. */
  wake?: string;
  wake_detail?: string;
}

/** An empty `foreman` unassigns. */
export function assignTicket(project: string, ticket: string, foreman: string) {
  return post<AssignResult>(`/api/tickets/${project}/${ticket}/assign`, { foreman });
}

export type ControlAction = 'pause' | 'cancel' | 'resume' | 'restore';

export interface ControlResult {
  ticket: string;
  control: string;
  status: string;
}

export function controlTicket(project: string, ticket: string, action: ControlAction, note = '') {
  return post<ControlResult>(`/api/tickets/${project}/${ticket}/control`, { action, note });
}

export type ApprovalAction = 'approve' | 'redirect' | 'drop';

export interface ApprovalResult {
  ticket: string;
  /** approved | redirected | dropped */
  approval: string;
}

/** `redirect` needs a note, or at least one inline comment; the server rejects
 *  a redirect carrying neither with a 400. */
export function resolveApproval(
  project: string,
  ticket: string,
  action: ApprovalAction,
  note = '',
) {
  return post<ApprovalResult>(`/api/tickets/${project}/${ticket}/approval`, { action, note });
}

export interface CommentDraft {
  target: 'requirement' | 'plan' | 'design' | 'prototype';
  anchor?: string;
  excerpt?: string;
  body: string;
}

/** Anchored feedback on one paragraph or one element. Accepted only while the
 *  record is pending; a resolved record answers with a 409. */
export function commentOnApproval(project: string, ticket: string, draft: CommentDraft) {
  return post<{ ticket: string; comment: string }>(
    `/api/tickets/${project}/${ticket}/approval/comment`,
    draft,
  );
}

/** Where `Open in new tab` points. Served mode only — over file:// the frame
 *  falls back to the HTML embedded in the snapshot. */
export function prototypeUrl(project: string, ticket: string) {
  return `/api/tickets/${project}/${ticket}/prototype`;
}

export interface SpawnResult {
  spawned: string;
  dir: string;
}

export function spawnForeman(dir: string, id = '', command = '') {
  return post<SpawnResult>('/api/foremen', { id, dir, command });
}
