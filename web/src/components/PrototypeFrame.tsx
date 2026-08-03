// The mock, inline.
//
// Lives in components/ because two screens show it: the ticket's Prototype tab
// reads it, and the approval banner reads it *and* lets the human point at a
// piece of it. Comment mode is therefore opt-in — `onPick` absent means a plain
// read-only frame, which is what a ticket with no approval record gets.

import { useEffect, useRef, useState } from 'react';
import { ExternalLink } from 'lucide-react';
import { prototypeUrl, type CommentDraft } from '../lib/api';
import type { TicketPrototype } from '../lib/data';
import { EmptyState } from './EmptyState';
import { snippet } from '../lib/format';
import { useControlPlane } from '../contexts/ControlContext';

/** Where a comment points, before the human has written its body. */
export type Anchor = Omit<CommentDraft, 'body'>;

/**
 * A CSS-ish path to one element of the mock, short enough to read in a
 * terminal. It is a hint, not a selector contract — the excerpt is what
 * survives the worker rewriting the prototype.
 */
function elementPath(el: Element) {
  const parts: string[] = [];
  let cur: Element | null = el;
  while (cur && cur.tagName !== 'BODY' && cur.tagName !== 'HTML' && parts.length < 4) {
    const node: Element = cur;
    let part = node.tagName.toLowerCase();
    if (node.id) {
      parts.unshift(`${part}#${node.id}`);
      break;
    }
    const cls = (node.getAttribute('class') ?? '').trim().split(/\s+/)[0];
    if (cls) part += `.${cls}`;
    const parent: Element | null = node.parentElement;
    if (parent) {
      const sibs: Element[] = Array.from(parent.children).filter(s => s.tagName === node.tagName);
      if (sibs.length > 1) part += `:nth(${sibs.indexOf(node) + 1})`;
    }
    parts.unshift(part);
    cur = parent;
  }
  return parts.join(' > ') || el.tagName.toLowerCase();
}

/**
 * `srcdoc` from the snapshot rather than the endpoint, because the file://
 * snapshot has no endpoint and the human reviewing a design deserves the same
 * screen either way. The sandbox stays at the design's `allow-same-origin`
 * only: the frame needs it to style itself, and withholding `allow-scripts`
 * means the mock cannot reach the API that is sitting on that origin.
 *
 * That same `allow-same-origin` is what makes comment mode possible: the parent
 * can reach into `contentDocument` to highlight and capture the element the
 * human clicks, while the mock's own scripts stay blocked.
 */
export function PrototypeFrame({
  project,
  ticket,
  prototype,
  commenting = false,
  onPick,
  height = 480,
}: {
  project: string;
  ticket: string;
  prototype: TicketPrototype | null;
  /** Comment mode. Ignored without `onPick` — there would be nowhere to send a pick. */
  commenting?: boolean;
  onPick?: (a: Anchor) => void;
  height?: number;
}) {
  const { canMutate } = useControlPlane();
  const frame = useRef<HTMLIFrameElement>(null);
  const [loaded, setLoaded] = useState(0);
  const picking = commenting && !!onPick;

  useEffect(() => {
    const doc = frame.current?.contentDocument;
    if (!picking || !onPick || !doc?.body) return;
    let lit: HTMLElement | null = null;
    const paint = (el: HTMLElement | null) => {
      if (lit) lit.style.outline = '';
      lit = el;
      // The mock is a separate document, so the parent's CSS variables mean
      // nothing inside it — this is the one place the accent is a literal.
      if (el) el.style.outline = '2px solid #5e6ad2';
    };
    const over = (e: Event) => paint(e.target as HTMLElement);
    const leave = () => paint(null);
    const click = (e: MouseEvent) => {
      // Capture phase and preventDefault: a click in comment mode is a pick,
      // never a navigation out of the mock.
      e.preventDefault();
      e.stopPropagation();
      const el = e.target as HTMLElement;
      onPick({ target: 'prototype', anchor: elementPath(el), excerpt: snippet(el.textContent ?? '') });
    };
    doc.addEventListener('mouseover', over, true);
    doc.addEventListener('mouseleave', leave, true);
    doc.addEventListener('click', click, true);
    doc.body.style.cursor = 'crosshair';
    return () => {
      doc.removeEventListener('mouseover', over, true);
      doc.removeEventListener('mouseleave', leave, true);
      doc.removeEventListener('click', click, true);
      paint(null);
      doc.body.style.cursor = '';
    };
  }, [picking, loaded, onPick]);

  if (!prototype) {
    return (
      <EmptyState
        title="No prototype"
        body="This change has no user-facing surface — which is itself a claim worth judging."
      />
    );
  }
  // Over file:// the endpoint does not exist, so the tab link points at the
  // file the snapshot named. Absolute path, so it resolves from any host page.
  const href = canMutate ? prototypeUrl(project, ticket) : `file://${prototype.path}`;

  return (
    <div>
      <div className="flex items-center justify-between gap-3 py-1">
        <span
          className="font-mono truncate"
          style={{ fontSize: 12, color: 'var(--text-muted)' }}
          title={prototype.path}
        >
          {prototype.path}
        </span>
        <a
          href={href}
          target="_blank"
          rel="noreferrer"
          className="inline-flex items-center gap-1 shrink-0 hover:underline"
          style={{ fontSize: 12, color: 'var(--accent)' }}
        >
          Open in new tab
          <ExternalLink size={12} aria-hidden="true" />
        </a>
      </div>
      {prototype.html ? (
        <iframe
          ref={frame}
          onLoad={() => setLoaded(n => n + 1)}
          title={`${ticket} prototype`}
          srcDoc={prototype.html}
          sandbox="allow-same-origin"
          style={{
            width: '100%',
            height,
            border: picking ? '1px solid var(--accent)' : '1px solid var(--border-hairline)',
            borderRadius: 'var(--radius-md)',
            backgroundColor: 'var(--surface-bg)',
          }}
        />
      ) : (
        <EmptyState
          title="Prototype too large to preview here"
          body={`${Math.round(prototype.bytes / 1024)}KB — open it in a tab to review it.`}
        />
      )}
    </div>
  );
}
