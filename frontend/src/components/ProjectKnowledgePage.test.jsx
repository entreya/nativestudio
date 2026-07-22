import React from 'react';
import { render, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import ProjectKnowledgePage from './ProjectKnowledgePage';

describe('ProjectKnowledgePage', () => {
  beforeEach(() => {
    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({
        indexed_files: 3,
        indexed_chunks: 8,
        symbol_count: 5,
        stale_count: 0,
        languages: { go: 3 },
        project_summary: { summary: JSON.stringify({ project_name: 'sample', project_type: 'service', architecture: 'Layered service', languages: ['Go'], frameworks: [] }) },
        file_summaries: [{ id: 'one', path: 'main.go', summary: JSON.stringify({ purpose: 'Starts the service' }) }],
        index_status: { status: 'completed', processed: 3, total: 3, skipped: 0, errors: 0 },
      }),
    });
  });

  it('renders persisted project knowledge and source actions', async () => {
    render(<ProjectKnowledgePage project={{ id: 'p1', name: 'sample', path: '/tmp/sample' }} onOpenFile={vi.fn()} onBack={vi.fn()} />);
    expect(await screen.findByText('Layered service')).toBeInTheDocument();
    expect(screen.getByText('Starts the service')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Open source' })).toBeInTheDocument();
    await waitFor(() => expect(globalThis.fetch).toHaveBeenCalledWith('/api/projects/p1/knowledge'));
  });
});
