import React from 'react';
import { Skeleton } from 'antd';

export function LandingSkeleton() {
  return (
    <div style={{ display: 'flex', height: '100vh', width: '100vw', alignItems: 'center', justifyContent: 'center', background: 'var(--studio-bg, #f7f4ed)' }}>
      <div style={{ width: 480, maxWidth: '90vw', padding: 32, background: 'var(--studio-surface, #fffdf8)', borderRadius: 16, border: '1px solid var(--studio-border, #d8d1c5)' }}>
        <Skeleton active avatar={{ shape: 'square' }} paragraph={{ rows: 1 }} title={false} />
        <Skeleton active paragraph={{ rows: 4 }} title={false} style={{ marginTop: 24 }} />
      </div>
    </div>
  );
}

export function EditorPageSkeleton() {
  return (
    <div style={{ display: 'flex', flexDirection: 'column', height: '100vh', width: '100vw', background: 'var(--studio-bg, #f7f4ed)' }}>
      <div style={{ height: 40, borderBottom: '1px solid var(--studio-border, #d8d1c5)', display: 'flex', alignItems: 'center', padding: '0 16px' }}>
        <Skeleton.Button active size="small" style={{ marginRight: 8 }} />
        <Skeleton.Button active size="small" />
      </div>
      <div style={{ display: 'flex', flex: 1, overflow: 'hidden' }}>
        <div style={{ width: 260, borderRight: '1px solid var(--studio-border, #d8d1c5)', padding: 16 }}>
          <Skeleton active paragraph={{ rows: 8 }} title={false} />
        </div>
        <div style={{ flex: 1, padding: 24 }}>
          <Skeleton active paragraph={{ rows: 10 }} />
        </div>
        <div style={{ width: 360, borderLeft: '1px solid var(--studio-border, #d8d1c5)', padding: 16 }}>
          <Skeleton active paragraph={{ rows: 6 }} title={false} />
        </div>
      </div>
    </div>
  );
}

export function KnowledgePageSkeleton() {
  return (
    <div style={{ display: 'flex', flexDirection: 'column', height: '100vh', width: '100vw', background: 'var(--studio-bg, #f7f4ed)' }}>
      <div style={{ height: 40, borderBottom: '1px solid var(--studio-border, #d8d1c5)' }} />
      <div style={{ padding: 32 }}>
        <Skeleton active title={{ width: 240 }} paragraph={{ rows: 2 }} />
        <div style={{ display: 'flex', gap: 16, marginTop: 24 }}>
          {[0, 1, 2, 3].map(i => (
            <div key={i} style={{ flex: 1, padding: 16, border: '1px solid var(--studio-border, #d8d1c5)', borderRadius: 8 }}>
              <Skeleton active paragraph={{ rows: 1 }} title={false} />
            </div>
          ))}
        </div>
        <Skeleton active paragraph={{ rows: 6 }} style={{ marginTop: 24 }} />
      </div>
    </div>
  );
}
