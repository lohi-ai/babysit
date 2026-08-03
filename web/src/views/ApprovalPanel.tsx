// The design checkpoint, moved out of a terminal prompt and onto a page.
//
// The whole point is that three buttons over an unread plan is not a review, so
// this panel puts the artifacts and the mock in front of the decision rather
// than beside it: the rubric says what to look for, the switcher reads the
// three documents inline, the frame shows the prototype, and only then does the
// bar ask. Everything above the bar stays readable in read-only mode — only the
// asking needs a server.
//
// Comment mode is the same idea one level down. "Redirect with a note" answers
// the whole plan at once; most design feedback is about *this* paragraph or
// *that* card, so the human points at it and the comment carries the quote to
// the worker. The single note stays — it is how you say something about the
// plan as a whole — and a redirect may now travel on comments alone.

import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { MessageSquarePlus } from 'lucide-react';
import {
  commentOnApproval,
  resolveApproval,
  type ApprovalAction,
} from '../lib/api';
import type { ApprovalComment, TicketApproval, TicketPrototype } from '../lib/data';
import { Button } from '../components/Button';
import { EmptyState } from '../components/EmptyState';
import { ErrorBox } from '../components/ErrorBox';
import { Field, inputStyle } from '../components/Field';
import { Markdown } from '../components/Markdown';
import { Modal } from '../components/Modal';
import { PrototypeFrame, type Anchor } from '../components/PrototypeFrame';
import { SectionHeader } from '../components/SectionHeader';
import { formatDate, formatRelative, snippet } from '../lib/format';
import { gateProps, useControlPlane, useMutation } from '../contexts/ControlContext';

// Verbatim from .claude/skills/foreman/SKILL.md § The design checkpoint — the
// same five lines the foreman fills before it greenlights anything. Restating
// them here in different words would create a second rubric to keep in sync.
const RUBRIC: [string, string][] = [
  ['Coverage', 'each acceptance criterion in the requirement maps to a named plan step / design element.'],
  ['Host-page consistency', 'the design names the sibling screen or component it borrows from. Any NEW: flag is a decision, not a detail.'],
  ['Reuse', 'existing components named; a new component carries a stated reason.'],
  ['Prototype inspected', 'the mock was actually opened — file existence is not evidence.'],
  ['Scope', 'nothing beyond the request wording.'],
];

type Artifact = 'requirement' | 'plan' | 'design';

export function ApprovalPanel({
  project,
  ticket,
  approval,
  requirement,
  plan,
  design,
  prototype,
}: {
  project: string;
  ticket: string;
  approval: TicketApproval;
  requirement: string | null;
  plan: string | null;
  design: string | null;
  prototype: TicketPrototype | null;
}) {
  const docs: Record<Artifact, string | null> = { requirement, plan, design };
  const order: Artifact[] = ['requirement', 'plan', 'design'];
  // Open on the design when there is one: the checkpoint is about the design,
  // and the requirement is the document the human already wrote.
  const [artifact, setArtifact] = useState<Artifact>(
    design ? 'design' : plan ? 'plan' : 'requirement',
  );
  const { canMutate, reason } = useControlPlane();
  const [commenting, setCommenting] = useState(false);
  const [draft, setDraft] = useState<Anchor | null>(null);
  const comments = approval.comments ?? [];
  const open = approval.state === 'pending';
  // Read-only mode reads the comments that are already there; it just cannot
  // add one, like every other mutation on this page.
  const canComment = open && canMutate;
  const commentsFor = (target: string) => comments.filter(c => c.target === target);

  const startComment = useCallback((a: Anchor) => setDraft(a), []);

  return (
    <div className="space-y-4">
      <SectionHeader title="What to check" defaultOpen={false}>
        <ul className="space-y-1 pt-1 pb-2" style={{ fontSize: 13, color: 'var(--text-secondary)' }}>
          {RUBRIC.map(([name, body]) => (
            <li key={name}>
              <span style={{ color: 'var(--text-primary)', fontWeight: 500 }}>{name}</span>
              {' — '}{body}
            </li>
          ))}
        </ul>
      </SectionHeader>

      {approval.note && (
        <div
          className="px-3 py-2"
          style={{
            fontSize: 13,
            color: 'var(--text-secondary)',
            backgroundColor: 'var(--surface-elevated)',
            borderRadius: 'var(--radius-md)',
          }}
        >
          <span style={{ color: 'var(--text-muted)' }}>
            {approval.requested_by || 'a worker'} asked, {formatRelative(approval.at)}:
          </span>{' '}
          {approval.note}
        </div>
      )}

      {/* Same underline-tab pattern as the detail's own strip, one level in. */}
      <div
        className="flex flex-wrap items-end justify-between gap-2"
        style={{ borderBottom: '1px solid var(--border-hairline)' }}
      >
        <nav className="flex gap-1 -mb-px">
          {order.map(key => {
            const available = !!docs[key];
            return (
              <button
                key={key}
                disabled={!available}
                onClick={() => setArtifact(key)}
                className="px-3 py-1.5 text-sm capitalize"
                style={{
                  borderBottom: '2px solid',
                  borderColor: artifact === key ? 'var(--accent)' : 'transparent',
                  color: !available
                    ? 'var(--text-muted)'
                    : artifact === key
                      ? 'var(--accent)'
                      : 'var(--text-secondary)',
                  cursor: available ? 'pointer' : 'not-allowed',
                  fontWeight: artifact === key ? 500 : 400,
                  opacity: available ? 1 : 0.5,
                }}
              >
                {key}
              </button>
            );
          })}
        </nav>
        {open && (
          <Button
            size="sm"
            variant={commenting ? 'primary' : 'ghost'}
            className="mb-1"
            onClick={() => {
              setDraft(null);
              setCommenting(v => !v);
            }}
            {...gateProps(canComment ? '' : reason)}
          >
            <MessageSquarePlus size={12} aria-hidden="true" />
            {commenting ? 'Done commenting' : 'Comment on a line'}
          </Button>
        )}
      </div>

      {docs[artifact] ? (
        <AnnotatedDoc
          source={docs[artifact] as string}
          target={artifact}
          comments={commentsFor(artifact)}
          commenting={commenting && canComment}
          onPick={startComment}
        />
      ) : (
        <EmptyState title={`No ${artifact}.`} />
      )}
      <CommentList comments={commentsFor(artifact)} />

      <PrototypeFrame
        project={project}
        ticket={ticket}
        prototype={prototype}
        commenting={commenting && canComment}
        onPick={startComment}
      />
      <CommentList comments={commentsFor('prototype')} />

      {draft && (
        <CommentComposer
          project={project}
          ticket={ticket}
          anchor={draft}
          onClose={() => setDraft(null)}
        />
      )}

      {open
        ? <DecisionBar project={project} ticket={ticket} comments={comments.length} />
        : <ResolvedRecord approval={approval} comments={comments} />}
    </div>
  );
}

/**
 * A markdown artifact whose top-level blocks can be pointed at.
 *
 * Anchors are assigned in an effect rather than baked into the renderer: the
 * markdown pipeline is shared with every other view, and a paragraph index is
 * only meaningful to this screen. Commented blocks keep an accent rail so the
 * feedback is visible on the document itself, not only in the list below it.
 */
function AnnotatedDoc({
  source,
  target,
  comments,
  commenting,
  onPick,
}: {
  source: string;
  target: Artifact;
  comments: ApprovalComment[];
  commenting: boolean;
  onPick: (a: Anchor) => void;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const anchored = useMemo(
    () => new Set(comments.map(c => c.anchor).filter(Boolean) as string[]),
    [comments],
  );

  useEffect(() => {
    const root = ref.current?.querySelector('.md');
    if (!root) return;
    Array.from(root.children).forEach((node, i) => {
      const el = node as HTMLElement;
      el.dataset.block = String(i);
      if (anchored.has(`block:${i}`)) el.dataset.commented = 'true';
      else delete el.dataset.commented;
    });
  }, [source, anchored]);

  const pick = (e: React.MouseEvent) => {
    if (!commenting) return;
    const el = (e.target as HTMLElement).closest('[data-block]') as HTMLElement | null;
    if (!el) return;
    e.preventDefault();
    onPick({ target, anchor: `block:${el.dataset.block}`, excerpt: snippet(el.textContent ?? '') });
  };

  return (
    <div ref={ref} className={commenting ? 'annotate annotating' : 'annotate'} onClick={pick}>
      <Markdown source={source} />
    </div>
  );
}

/**
 * Write the comment for the thing just clicked. It quotes the pick back, since
 * by the time the human is typing they have scrolled and the highlight may be
 * off screen — and the quote is what the worker will read, so it should be
 * visible before it is sent.
 */
function CommentComposer({
  project,
  ticket,
  anchor,
  onClose,
}: {
  project: string;
  ticket: string;
  anchor: Anchor;
  onClose: () => void;
}) {
  const { run, pending, error } = useMutation();
  const [body, setBody] = useState('');

  const save = async () => {
    const ok = await run(
      () => commentOnApproval(project, ticket, { ...anchor, body: body.trim() }),
      'Comment added',
    );
    if (ok) {
      setBody('');
      onClose();
    }
  };

  return (
    <div
      className="sticky bottom-16 px-3 py-3 space-y-2"
      style={{
        border: '1px solid var(--accent)',
        borderRadius: 'var(--radius-md)',
        backgroundColor: 'var(--surface-elevated)',
      }}
    >
      <div style={{ fontSize: 12, color: 'var(--text-muted)' }}>
        Commenting on <span style={{ textTransform: 'capitalize' }}>{anchor.target}</span>
        {anchor.anchor && <span className="font-mono"> · {anchor.anchor}</span>}
      </div>
      {anchor.excerpt && (
        <blockquote
          className="truncate"
          style={{
            fontSize: 13,
            color: 'var(--text-secondary)',
            borderLeft: '2px solid var(--border-emphasis)',
            paddingLeft: 8,
          }}
        >
          {anchor.excerpt}
        </blockquote>
      )}
      <Field label="What should change here?">
        {id => (
          <textarea
            id={id}
            rows={2}
            autoFocus
            style={{ ...inputStyle, resize: 'vertical' }}
            value={body}
            onChange={e => setBody(e.target.value)}
            onKeyDown={e => {
              if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) void save();
              if (e.key === 'Escape') onClose();
            }}
          />
        )}
      </Field>
      {error && <ErrorBox title="Could not save the comment" body={error} />}
      <div className="flex items-center gap-2">
        <Button variant="primary" onClick={save} disabled={pending || !body.trim()}>
          {pending ? 'Saving…' : 'Add comment'}
        </Button>
        <Button onClick={onClose}>Cancel</Button>
        <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>⌘↵ to save</span>
      </div>
    </div>
  );
}

/** The comments already on one surface. Empty renders nothing — an empty list
 *  header on every artifact would be noise on the common path. */
function CommentList({ comments }: { comments: ApprovalComment[] }) {
  if (comments.length === 0) return null;
  return (
    <ul className="space-y-2">
      {comments.map(c => (
        <li
          key={c.id}
          className="px-3 py-2"
          style={{
            borderLeft: '2px solid var(--accent)',
            backgroundColor: 'var(--surface-elevated)',
            borderRadius: 'var(--radius-sm)',
          }}
        >
          {c.excerpt && (
            <div className="truncate" style={{ fontSize: 12, color: 'var(--text-muted)' }}>
              “{c.excerpt}”
            </div>
          )}
          <div style={{ fontSize: 13, color: 'var(--text-primary)' }}>{c.body}</div>
          <div style={{ fontSize: 12, color: 'var(--text-muted)' }}>
            {c.actor || 'unknown'}
            {' · '}
            <span title={formatDate(c.at)}>{formatRelative(c.at)}</span>
            {c.anchor && <span className="font-mono"> · {c.anchor}</span>}
          </div>
        </li>
      ))}
    </ul>
  );
}

const RESOLVED_LABEL: Record<string, string> = {
  approved: 'Approved',
  redirected: 'Redirected',
  dropped: 'Dropped',
};

function ResolvedRecord({
  approval,
  comments,
}: {
  approval: TicketApproval;
  comments: ApprovalComment[];
}) {
  const r = approval.resolved;
  const outcome = r?.outcome ?? approval.state;
  return (
    <div
      className="px-4 py-3 space-y-1"
      style={{
        borderTop: '1px solid var(--border-hairline)',
        backgroundColor: 'var(--surface-elevated)',
        borderRadius: 'var(--radius-md)',
      }}
    >
      <div style={{ fontSize: 13, color: 'var(--text-primary)', fontWeight: 500 }}>
        {RESOLVED_LABEL[outcome] ?? outcome}
        {r && (
          <span style={{ color: 'var(--text-muted)', fontWeight: 400 }}>
            {' by '}{r.actor || 'unknown'}{' · '}
            <span title={formatDate(r.at)}>{formatRelative(r.at)}</span>
          </span>
        )}
      </div>
      {r?.note && (
        <div style={{ fontSize: 13, color: 'var(--text-secondary)' }}>{r.note}</div>
      )}
      {comments.length > 0 && (
        <div style={{ fontSize: 13, color: 'var(--text-secondary)' }}>
          {comments.length === 1 ? '1 inline comment' : `${comments.length} inline comments`} went
          with it.
        </div>
      )}
      <div style={{ fontSize: 12, color: 'var(--text-muted)' }}>
        The foreman reads this from disk on its next tick — nothing here waits on it.
      </div>
    </div>
  );
}

/**
 * Sticky so the decision stays reachable while the human scrolls the plan —
 * scrolling back up to answer is how a review turns into a rubber stamp.
 */
function DecisionBar({
  project,
  ticket,
  comments,
}: {
  project: string;
  ticket: string;
  comments: number;
}) {
  const { reason } = useControlPlane();
  const { run, pending, error } = useMutation();
  const [redirecting, setRedirecting] = useState(false);
  const [dropOpen, setDropOpen] = useState(false);
  const [note, setNote] = useState('');

  const decide = (action: ApprovalAction, body = '') =>
    run(() => resolveApproval(project, ticket, action, body), `${ticket} ${action}ed`);

  const redirect = async () => {
    if (await decide('redirect', note.trim())) {
      setRedirecting(false);
      setNote('');
    }
  };

  return (
    <div
      className="sticky bottom-0 -mx-2 px-2 pt-3 pb-3 space-y-3"
      style={{
        borderTop: '1px solid var(--border-hairline)',
        backgroundColor: 'var(--surface-elevated)',
      }}
    >
      {error && !dropOpen && <ErrorBox title="Could not record the decision" body={error} />}

      {redirecting && (
        <Field
          label="What should change?"
          hint={
            comments > 0
              ? `Optional — ${comments === 1 ? '1 inline comment goes' : `${comments} inline comments go`} with this redirect. Add a line here for anything that is about the plan as a whole.`
              : 'Required — the foreman acts on this note, so a redirect without one is a dead end.'
          }
        >
          {id => (
            <textarea
              id={id}
              rows={3}
              autoFocus
              style={{ ...inputStyle, resize: 'vertical' }}
              value={note}
              onChange={e => setNote(e.target.value)}
            />
          )}
        </Field>
      )}

      <div className="flex flex-wrap items-center gap-2">
        {redirecting ? (
          <>
            <Button
              size="lg"
              variant="primary"
              onClick={redirect}
              disabled={pending || (!note.trim() && comments === 0)}
            >
              {pending ? 'Sending…' : 'Send redirect'}
            </Button>
            <Button size="lg" onClick={() => setRedirecting(false)}>Back</Button>
          </>
        ) : (
          <>
            <Button
              size="lg"
              variant="primary"
              onClick={() => decide('approve')}
              disabled={pending}
              {...gateProps(reason)}
            >
              {pending ? 'Approving…' : 'Approve'}
            </Button>
            <Button size="lg" onClick={() => setRedirecting(true)} {...gateProps(reason)}>
              Redirect with a note
            </Button>
            <Button
              size="lg"
              onClick={() => setDropOpen(true)}
              style={{ color: 'var(--status-blocked-text)' }}
              {...gateProps(reason)}
            >
              Drop
            </Button>
          </>
        )}
      </div>

      <Modal
        open={dropOpen}
        onClose={() => setDropOpen(false)}
        title={`Drop the plan for ${ticket}?`}
        actions={
          <>
            <Button size="lg" onClick={() => setDropOpen(false)}>Keep it open</Button>
            <Button size="lg" variant="primary" onClick={() => decide('drop')} disabled={pending}>
              {pending ? 'Dropping…' : 'Drop'}
            </Button>
          </>
        }
      >
        <p className="text-sm" style={{ color: 'var(--text-secondary)' }}>
          The foreman stops work on this ticket; nothing is deleted. The requirement, plan,
          design and prototype all stay on disk, and a new decision can be published later.
        </p>
        {error && <ErrorBox title="Could not drop it" body={error} />}
      </Modal>
    </div>
  );
}
