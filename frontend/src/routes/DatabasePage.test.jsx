import React from 'react';
import { render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { describe, expect, it, vi } from 'vitest';
import DatabasePage from './DatabasePage';

const activeProject = { id: 'p1', name: 'sample', path: '/tmp/sample' };

vi.mock('../state/useAppState', () => ({
  useAppState: () => ({
    studioStyle: {},
    studioClassName: '',
    activeProject,
    handleNavigate: vi.fn(),
    handleOpenFile: vi.fn(),
    handleCreateProject: vi.fn(),
    setScanNotificationMinimized: vi.fn(),
    // Matches AppStateContext's real initial shape — indexStatus is never
    // actually null in the running app, so this stays representative of
    // production rather than testing an unreachable state.
    indexStatus: { status: 'idle', processed: 0, total: 0, skipped: 0, errors: 0 },
    scanNotificationMinimized: true,
  }),
}));

vi.mock('../hooks/useRouteSync', () => ({
  useProjectRouteSync: () => activeProject.id,
}));

describe('DatabasePage', () => {
  it('renders on first paint without throwing, before the tables fetch resolves', () => {
    // Live bug: tables starts as null (distinguishing "not fetched yet" from
    // "fetched, genuinely empty"), and a metric card once read tables.length
    // directly with no guard, crashing this very first render and unmounting
    // the whole page. render() must not throw — a fetch that never resolves
    // is exactly the window during which that crash happened.
    globalThis.fetch = vi.fn(() => new Promise(() => {}));

    expect(() =>
      render(
        <MemoryRouter initialEntries={['/projects/p1/db']}>
          <DatabasePage />
        </MemoryRouter>
      )
    ).not.toThrow();

    expect(screen.getByText('Database Dashboard')).toBeInTheDocument();
  });

  it('opens the first table as a tab once the table list loads', async () => {
    globalThis.fetch = vi.fn((url) => {
      if (url.includes('/db/tables/')) {
        return Promise.resolve({
          ok: true,
          json: async () => ({ columns: [{ name: 'id', type: 'TEXT' }], rows: [{ id: '1' }], total_rows: 1 }),
        });
      }
      if (url.includes('/db/tables')) {
        return Promise.resolve({ ok: true, json: async () => ['sessions', 'messages'] });
      }
      return Promise.resolve({ ok: true, json: async () => ({}) });
    });

    render(
      <MemoryRouter initialEntries={['/projects/p1/db']}>
        <DatabasePage />
      </MemoryRouter>
    );

    // Auto-opens "sessions" (the first table) as a closable tab.
    await waitFor(() => expect(screen.getAllByText('sessions').length).toBeGreaterThan(0));
    expect(screen.getAllByRole('button', { name: /New Query/i }).length).toBeGreaterThan(0);
  });
});
