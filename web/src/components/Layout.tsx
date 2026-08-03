import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import { Home as HomeIcon, Activity, HardHat, Inbox, Workflow, Zap, Calendar, BarChart3, ChevronDown, ChevronRight, MoreHorizontal } from 'lucide-react';
import type { Meta, Snapshot } from '../lib/data';
import { formatDate } from '../lib/format';
import { ProjectSwitcher } from './ProjectSwitcher';
import { Kbd } from './Kbd';
import { CommandPalette } from './CommandPalette';
import { ThemeToggle } from './ThemeToggle';
import { ShortcutsHelp } from './ShortcutsHelp';
import { FocusScopeContext, FilterKeyContext, useGlobalKeyboard, type FocusScope, type FilterKeyRef } from '../lib/keyboard';
import { useFilterOptional } from '../contexts/FilterContext';
import { pendingApprovals } from './WaitingOnYou';

// Two lists, not one: the dashboard is for watching work, so the routes you act
// on stay in the nav and the five reference views fold behind a disclosure.
// Foremen is in the first list because it is a control surface, not a report —
// it is where a foreman gets spawned, and a stale one is only visible from
// there. Nothing is unreachable — `G <key>` still routes to every one of them
// (lib/keyboard.ts owns that map independently), and the panel opens itself
// when the active route lives inside it.
const PRIMARY_ITEMS = [
  { hash: '#/',              label: 'Home',         kbd: 'H', Icon: HomeIcon },
  { hash: '#/tickets',       label: 'Tickets',      kbd: 'T', Icon: Inbox },
  { hash: '#/foremen',       label: 'Foremen',      kbd: 'F', Icon: HardHat },
] as const;

const MORE_ITEMS = [
  { hash: '#/live',          label: 'Live',         kbd: 'L', Icon: Activity },
  { hash: '#/decisions',     label: 'Decisions',    kbd: 'D', Icon: Workflow },
  { hash: '#/skill-events',  label: 'Skill events', kbd: 'S', Icon: Zap },
  { hash: '#/timeline',      label: 'Timeline',     kbd: 'M', Icon: Calendar },
  { hash: '#/analytics',     label: 'Analytics',    kbd: 'A', Icon: BarChart3 },
] as const;

type NavItem = (typeof PRIMARY_ITEMS)[number] | (typeof MORE_ITEMS)[number];

export function Layout({
  meta,
  snapshot,
  active,
  children,
}: {
  meta: Meta | null;
  snapshot?: Snapshot | null;
  active: string;
  children: ReactNode;
}) {
  const [paletteOpen, setPaletteOpen] = useState(false);
  const [helpOpen, setHelpOpen] = useState(false);

  const scopeRef = useRef<FocusScope | null>(null);
  const filterKeyRef = useRef<(() => void) | null>(null) as FilterKeyRef;

  const togglePalette = useCallback(() => setPaletteOpen(o => !o), []);
  const toggleHelp = useCallback(() => setHelpOpen(o => !o), []);
  const closeOverlays = useCallback(() => {
    setPaletteOpen(false);
    setHelpOpen(false);
  }, []);

  const actions = useMemo(() => ({ togglePalette, toggleHelp, closeOverlays }), [togglePalette, toggleHelp, closeOverlays]);

  useGlobalKeyboard(scopeRef, actions, filterKeyRef);

  const activeRoute = active.split('?')[0];

  const isActive = (hash: string) =>
    hash === '#/'
      ? (activeRoute === '#/' || activeRoute === '' || activeRoute === '#')
      : activeRoute.startsWith(hash);

  // Opened by hand, or opened for you on arrival because the route the human is
  // on lives in there — landing on Analytics with an empty nav would read as
  // "you are nowhere". Arrival *opens* it rather than pinning it open: a button
  // that cannot close what it opened is a button that does nothing on six of
  // the eight routes.
  const insideMore = MORE_ITEMS.some(i => isActive(i.hash));
  const [moreShown, setMoreShown] = useState(insideMore);
  useEffect(() => {
    if (insideMore) setMoreShown(true);
  }, [insideMore]);

  const filter = useFilterOptional();
  const projectParam =
    filter && filter.state.project !== 'all' && filter.state.project
      ? `project=${encodeURIComponent(filter.state.project)}`
      : '';
  const withProject = (hash: string) =>
    projectParam ? `${hash}${hash.includes('?') ? '&' : '?'}${projectParam}` : hash;

  // The second entry point to the design checkpoint. Home carries the first,
  // but the human may never open Home — and a decision nobody notices blocks a
  // worker for as long as it goes unnoticed.
  const pendingApprovalCount = useMemo(() => {
    if (!snapshot) return 0;
    const project = filter?.state.project ?? 'all';
    return Object.entries(snapshot.projects)
      .filter(([slug]) => project === 'all' || slug === project)
      .reduce((n, [, p]) => n + pendingApprovals(p.tickets ?? []).length, 0);
  }, [snapshot, filter]);

  return (
    <FocusScopeContext.Provider value={scopeRef}>
      <FilterKeyContext.Provider value={filterKeyRef}>
      <div className="min-h-screen flex" style={{ backgroundColor: 'var(--surface-bg)', color: 'var(--text-primary)' }}>
        <nav
          className="w-60 flex flex-col p-4 shrink-0 sticky top-0 self-start h-screen overflow-y-auto"
          style={{ backgroundColor: 'var(--surface-nav)', color: 'var(--text-nav)' }}
        >
          {snapshot ? (
            <ProjectSwitcher snapshot={snapshot} />
          ) : (
            <div className="mb-4">
              <div
                className="text-base font-semibold tracking-tight"
                style={{ color: 'var(--text-nav-active)', fontFamily: 'var(--font-display)' }}
              >
                babysit
              </div>
              <div className="text-xs" style={{ color: 'var(--text-muted)' }}>dashboard</div>
            </div>
          )}

          <div className="flex-1">
            <ul className="space-y-0.5">
              {PRIMARY_ITEMS.map(item => (
                <li key={item.hash}>
                  <NavLink
                    item={item}
                    href={withProject(item.hash)}
                    isOn={isActive(item.hash)}
                    badge={item.hash === '#/tickets' ? pendingApprovalCount : 0}
                  />
                </li>
              ))}
            </ul>

            <button
              type="button"
              onClick={() => setMoreShown(o => !o)}
              aria-expanded={moreShown}
              aria-controls="nav-more"
              className="group flex items-center justify-between w-full px-3 py-1.5 mt-0.5 text-sm"
              style={{
                color: moreShown ? 'var(--text-nav-active)' : 'var(--text-nav)',
                borderRadius: 'var(--radius-sm)',
                backgroundColor: 'transparent',
                transition: 'background-color var(--dur-fast) var(--ease-out), color var(--dur-fast) var(--ease-out)',
              }}
              onMouseEnter={e => { e.currentTarget.style.backgroundColor = 'var(--surface-hover)'; }}
              onMouseLeave={e => { e.currentTarget.style.backgroundColor = 'transparent'; }}
            >
              <span className="flex items-center gap-2 min-w-0">
                <MoreHorizontal size={14} strokeWidth={1.75} aria-hidden="true" />
                <span className="truncate">More</span>
              </span>
              {moreShown
                ? <ChevronDown size={14} strokeWidth={1.75} aria-hidden="true" />
                : <ChevronRight size={14} strokeWidth={1.75} aria-hidden="true" />}
            </button>

            <ul
              id="nav-more"
              hidden={!moreShown}
              className="space-y-0.5 mt-0.5 ml-3 pl-2"
              style={{ borderLeft: '1px solid var(--border-nav)' }}
            >
              {MORE_ITEMS.map(item => (
                <li key={item.hash}>
                  <NavLink item={item} href={withProject(item.hash)} isOn={isActive(item.hash)} badge={0} />
                </li>
              ))}
            </ul>
          </div>

          <button
            type="button"
            onClick={() => setPaletteOpen(true)}
            className="mt-4 flex items-center gap-1.5 px-3 py-1.5 rounded text-xs w-full"
            style={{
              color: 'var(--text-muted)',
              borderRadius: 'var(--radius-sm)',
              transition: 'background-color var(--dur-fast) var(--ease-out), color var(--dur-fast) var(--ease-out)',
            }}
            onMouseEnter={e => {
              e.currentTarget.style.backgroundColor = 'var(--surface-hover)';
              e.currentTarget.style.color = 'var(--text-nav)';
            }}
            onMouseLeave={e => {
              e.currentTarget.style.backgroundColor = 'transparent';
              e.currentTarget.style.color = 'var(--text-muted)';
            }}
            title="Open command palette"
          >
            <Kbd dim>Cmd</Kbd>
            <span>+</span>
            <Kbd dim>K</Kbd>
            <span className="ml-1">to search</span>
          </button>

          <button
            type="button"
            onClick={() => setHelpOpen(true)}
            className="flex items-center gap-1.5 px-3 py-1 rounded text-xs w-full"
            style={{
              color: 'var(--text-muted)',
              transition: 'color var(--dur-fast) var(--ease-out)',
            }}
            onMouseEnter={e => {
              e.currentTarget.style.color = 'var(--text-nav)';
            }}
            onMouseLeave={e => {
              e.currentTarget.style.color = 'var(--text-muted)';
            }}
            title="Keyboard shortcuts"
          >
            <Kbd dim>?</Kbd>
            <span className="ml-1">shortcuts</span>
          </button>

          <div className="mt-1">
            <ThemeToggle />
          </div>

          {meta && (
            <div
              className="mt-2 pt-4 text-xs space-y-0.5"
              style={{ borderTop: '1px solid var(--border-nav)', color: 'var(--text-muted)' }}
            >
              {meta.active_project && (
                <div className="font-medium truncate" style={{ color: 'var(--text-nav)' }}>{meta.active_project}</div>
              )}
              {meta.babysit_version && <div>v{meta.babysit_version}</div>}
              <div>snapshot {formatDate(meta.generated_at || meta.snapshot_at)}</div>
            </div>
          )}
        </nav>
        <main className="flex-1 flex flex-col min-w-0 overflow-x-clip">{children}</main>
      </div>

      <CommandPalette
        open={paletteOpen}
        onClose={() => setPaletteOpen(false)}
        snapshot={snapshot}
      />
      <ShortcutsHelp open={helpOpen} onClose={() => setHelpOpen(false)} />
      </FilterKeyContext.Provider>
    </FocusScopeContext.Provider>
  );
}

/** One nav row. Shared by the primary list and the More panel so a route reads
 *  the same wherever it sits. */
function NavLink({
  item,
  href,
  isOn,
  badge,
}: {
  item: NavItem;
  href: string;
  isOn: boolean;
  badge: number;
}) {
  const Icon = item.Icon;
  return (
    <a
      href={href}
      className="group flex items-center justify-between px-3 py-1.5 rounded text-sm"
      style={{
        backgroundColor: isOn ? 'var(--surface-nav-elevated)' : 'transparent',
        color: isOn ? 'var(--text-nav-active)' : 'var(--text-nav)',
        borderRadius: 'var(--radius-sm)',
        transition: 'background-color var(--dur-fast) var(--ease-out), color var(--dur-fast) var(--ease-out)',
      }}
      onMouseEnter={e => {
        if (!isOn) e.currentTarget.style.backgroundColor = 'var(--surface-hover)';
      }}
      onMouseLeave={e => {
        if (!isOn) e.currentTarget.style.backgroundColor = 'transparent';
      }}
    >
      <span className="flex items-center gap-2 min-w-0">
        <Icon size={14} strokeWidth={1.75} aria-hidden="true" />
        <span className="truncate">{item.label}</span>
      </span>
      <span className="flex items-center gap-1.5 shrink-0">
        {badge > 0 && (
          <span
            className="font-mono"
            aria-label={`${badge} waiting on you`}
            style={{
              fontSize: 11,
              padding: '0 6px',
              lineHeight: '16px',
              borderRadius: 'var(--radius-sm)',
              backgroundColor: 'var(--accent)',
              color: 'var(--accent-fg)',
              fontVariantNumeric: 'tabular-nums',
            }}
          >
            {badge}
          </span>
        )}
        <span
          className="opacity-0 group-hover:opacity-60"
          style={{ transition: 'opacity var(--dur-fast) var(--ease-out)' }}
        >
          <Kbd dim>G {item.kbd}</Kbd>
        </span>
      </span>
    </a>
  );
}
