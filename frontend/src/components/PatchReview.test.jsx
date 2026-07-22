import React from 'react';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import PatchReview from './PatchReview';

globalThis.ResizeObserver = class ResizeObserver {
  observe() {}
  unobserve() {}
  disconnect() {}
};

afterEach(cleanup);

const patches = [
  {
    patch_id: 'patch-1',
    file_path: 'src/example.js',
    operation: 'modify',
    diff: '--- a/src/example.js\n+++ b/src/example.js\n@@ -1,1 +1,1 @@\n-old\n+new\n',
  },
  {
    patch_id: 'patch-2',
    file_path: 'src/new.js',
    operation: 'create',
    diff: '--- a/src/new.js\n+++ b/src/new.js\n@@ -0,0 +1,1 @@\n+new\n',
  },
];

describe('PatchReview', () => {
  it('renders staged diffs and resolves a single patch', async () => {
    const approve = vi.fn().mockResolvedValue(undefined);
    render(
      <PatchReview
        patches={patches}
        onApprove={approve}
        onReject={vi.fn()}
        onApproveAll={vi.fn()}
        onRejectAll={vi.fn()}
      />,
    );

    expect(screen.getByText('2 files changed — review before applying')).toBeInTheDocument();
    expect(screen.getByText('src/example.js')).toBeInTheDocument();
    fireEvent.click(screen.getAllByRole('button', { name: /approve$/i })[0]);
    await waitFor(() => expect(approve).toHaveBeenCalledWith('patch-1'));
  });

  it('shows an actionable error when approval fails', async () => {
    const approve = vi.fn().mockRejectedValue(new Error('file changed since staging'));
    render(
      <PatchReview
        patches={[patches[0]]}
        onApprove={approve}
        onReject={vi.fn()}
        onApproveAll={vi.fn()}
        onRejectAll={vi.fn()}
      />,
    );

    fireEvent.click(screen.getByRole('button', { name: /approve$/i }));
    expect(await screen.findByText('file changed since staging')).toBeInTheDocument();
  });
});
