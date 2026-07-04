import { useCallback, useMemo, useRef, useState, type ReactNode } from 'react';
import { Home as HomeIcon, Activity, Inbox, Workflow, Zap, Calendar, BarChart3 } from 'lucide-react';
import type { Meta, Snapshot } from '../lib/data';
import { formatDate } from '../lib/format';
import { ProjectSwitcher } from './ProjectSwitcher';
import { Kbd } from './Kbd';
import { CommandPalette } from './CommandPalette';
import { ThemeToggle } from './ThemeToggle';
import { ShortcutsHelp } from './ShortcutsHelp';
import { FocusScopeContext, FilterKeyContext, useGlobalKeyboard, type FocusScope, type FilterKeyRef } from '../lib/keyboard';
import { useFilterOptional } from '../contexts/FilterContext';

const NAV_ITEMS = [
  { hash: '#/',              label: 'Home',         kbd: 'H', Icon: HomeIcon },
  { hash: '#/live',          label: 'Live',         kbd: 'L', Icon: Activity },
  { hash: '#/tickets',       label: 'Tickets',      kbd: 'T', Icon: Inbox },
  { hash: '#/decisions',     label: 'Decisions',    kbd: 'D', Icon: Workflow },
  { hash: '#/skill-events',  label: 'Skill events', kbd: 'S', Icon: Zap },
  { hash: '#/timeline',      label: 'Timeline',     kbd: 'M', Icon: Calendar },
  { hash: '#/analytics',     label: 'Analytics',    kbd: 'A', Icon: BarChart3 },
] as const;

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

  const filter = useFilterOptional();
  const projectParam =
    filter && filter.state.project !== 'all' && filter.state.project
      ? `project=${encodeURIComponent(filter.state.project)}`
      : '';
  const withProject = (hash: string) =>
    projectParam ? `${hash}${hash.includes('?') ? '&' : '?'}${projectParam}` : hash;

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

          <ul className="space-y-0.5 flex-1">
            {NAV_ITEMS.map(item => {
              const isOn = isActive(item.hash);
              const Icon = item.Icon;
              return (
                <li key={item.hash}>
                  <a
                    href={withProject(item.hash)}
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
                    <span
                      className="opacity-0 group-hover:opacity-60"
                      style={{ transition: 'opacity var(--dur-fast) var(--ease-out)' }}
                    >
                      <Kbd dim>G {item.kbd}</Kbd>
                    </span>
                  </a>
                </li>
              );
            })}
          </ul>

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
