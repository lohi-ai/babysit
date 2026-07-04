import { useEffect, useState } from 'react';

type Theme = 'light' | 'dark';

function readInitialTheme(): Theme {
  const attr = document.documentElement.getAttribute('data-theme');
  return attr === 'dark' ? 'dark' : 'light';
}

export function ThemeToggle() {
  const [theme, setTheme] = useState<Theme>(readInitialTheme);

  useEffect(() => {
    document.documentElement.setAttribute('data-theme', theme);
    try {
      localStorage.setItem('bbs:theme', theme);
    } catch (_) {
      // ignore quota / disabled storage
    }
  }, [theme]);

  const isDark = theme === 'dark';
  return (
    <button
      type="button"
      role="switch"
      aria-checked={isDark}
      aria-label="Toggle dark mode"
      onClick={() => setTheme(isDark ? 'light' : 'dark')}
      className="flex items-center justify-between gap-2 px-3 py-1.5 rounded text-xs w-full transition-colors"
      style={{
        color: 'var(--text-nav)',
        backgroundColor: 'transparent',
      }}
      onMouseEnter={e => {
        e.currentTarget.style.backgroundColor = 'var(--surface-nav-elevated)';
      }}
      onMouseLeave={e => {
        e.currentTarget.style.backgroundColor = 'transparent';
      }}
      title={isDark ? 'Switch to light' : 'Switch to dark'}
    >
      <span className="flex items-center gap-2">
        <span aria-hidden="true">{isDark ? '◐' : '◑'}</span>
        <span>{isDark ? 'Dark' : 'Light'}</span>
      </span>
      <span
        aria-hidden="true"
        className="inline-flex items-center w-7 h-3.5 rounded-full px-0.5"
        style={{
          backgroundColor: isDark ? 'var(--accent)' : 'var(--border-emphasis)',
          justifyContent: isDark ? 'flex-end' : 'flex-start',
        }}
      >
        <span
          className="block w-2.5 h-2.5 rounded-full"
          style={{ backgroundColor: 'var(--p-white, #fff)' }}
        />
      </span>
    </button>
  );
}
