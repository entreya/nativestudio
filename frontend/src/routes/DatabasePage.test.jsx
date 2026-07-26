import React from 'react';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { beforeEach, describe, expect, it, vi } from 'vitest';
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
  beforeEach(() => {
    // Never resolves during the test — this is the regression scenario:
    // the component must render its very first pass (tables still at its
    // initial null value, before any fetch has settled) without throwing.
    globalThis.fetch = vi.fn(() => new Promise(() => {}));
  });

  it('renders the metric cards on first paint, before the tables fetch resolves', () => {
    // Live bug: tables starts as null (distinguishing "not fetched yet" from
    // "fetched, genuinely empty"), but the Total Tables card read
    // tables.length directly with no guard, crashing this very first render
    // and unmounting the whole page. render() must not throw.
    expect(() =>
      render(
        <MemoryRouter initialEntries={['/projects/p1/db']}>
          <DatabasePage />
        </MemoryRouter>
      )
    ).not.toThrow();

    expect(screen.getByText('Total Tables')).toBeInTheDocument();
    expect(screen.getByText('Rows in Table')).toBeInTheDocument();
  });
});
