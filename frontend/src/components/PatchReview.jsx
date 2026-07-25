import React, { useState } from 'react';
import { Alert, Button, Card, Space, Tag, Typography } from 'antd';
import { CheckOutlined, CloseOutlined, FileOutlined } from '@ant-design/icons';

const { Text } = Typography;

function DiffView({ diff }) {
  return (
    <pre style={{ margin: 0, maxHeight: 400, overflow: 'auto', borderRadius: 6, background: 'var(--studio-panel, #f5f1e9)', fontSize: 11.5, lineHeight: 1.55 }}>
      {(diff || '').split('\n').map((line, index) => {
        let style = { display: 'block', minHeight: '1.55em', padding: '0 10px', color: 'var(--studio-text, #2f2a26)' };
        if (line.startsWith('@@')) style = { ...style, color: 'var(--studio-accent, #315f86)', background: 'color-mix(in srgb, var(--studio-accent, #315f86) 10%, transparent)', fontWeight: 600 };
        else if (line.startsWith('+++') || line.startsWith('---')) style = { ...style, color: 'var(--studio-muted, #746b63)', fontWeight: 600 };
        else if (line.startsWith('+')) style = { ...style, color: 'var(--studio-success, #166534)', background: 'color-mix(in srgb, var(--studio-success, #166534) 10%, transparent)' };
        else if (line.startsWith('-')) style = { ...style, color: 'var(--studio-danger, #991b1b)', background: 'color-mix(in srgb, var(--studio-danger, #991b1b) 10%, transparent)' };
        return <code key={`${index}-${line}`} style={style}>{line || ' '}</code>;
      })}
    </pre>
  );
}

export default function PatchReview({ patches, onApprove, onReject, onApproveAll, onRejectAll }) {
  const [resolving, setResolving] = useState([]);
  const [error, setError] = useState('');
  const run = async (ids, action) => {
    setError('');
    setResolving(current => [...new Set([...current, ...ids])]);
    try {
      await action();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Could not resolve the staged change.');
    } finally {
      setResolving(current => current.filter(id => !ids.includes(id)));
    }
  };

  if (!patches?.length) return null;
  const allIDs = patches.map(patch => patch.patch_id);
  return (
    <section aria-label="Staged file changes" style={{ width: '100%', display: 'flex', flexDirection: 'column', gap: 10 }}>
      {error && <Alert type="error" showIcon message={error} closable onClose={() => setError('')} />}
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 8, flexWrap: 'wrap' }}>
        <Text strong>{patches.length} file{patches.length === 1 ? '' : 's'} changed — review before applying</Text>
        {patches.length > 1 && (
          <Space size={6}>
            <Button size="small" type="primary" icon={<CheckOutlined />} loading={resolving.length > 0} onClick={() => run(allIDs, onApproveAll)}>Approve All</Button>
            <Button size="small" danger icon={<CloseOutlined />} disabled={resolving.length > 0} onClick={() => run(allIDs, onRejectAll)}>Reject All</Button>
          </Space>
        )}
      </div>
      {patches.map(patch => (
        <Card
          key={patch.patch_id}
          size="small"
          title={<Space size={7}><FileOutlined /><Text ellipsis style={{ maxWidth: 420 }}>{patch.file_path}</Text><Tag color={patch.operation === 'delete' ? 'error' : patch.operation === 'create' ? 'success' : 'processing'}>{patch.operation.toUpperCase()}</Tag></Space>}
          styles={{ body: { padding: 10 } }}
        >
          <DiffView diff={patch.diff} />
          <Space size={7} style={{ display: 'flex', justifyContent: 'flex-end', marginTop: 10 }}>
            <Button size="small" type="primary" icon={<CheckOutlined />} loading={resolving.includes(patch.patch_id)} onClick={() => run([patch.patch_id], () => onApprove(patch.patch_id))}>Approve</Button>
            <Button size="small" danger icon={<CloseOutlined />} disabled={resolving.includes(patch.patch_id)} onClick={() => run([patch.patch_id], () => onReject(patch.patch_id))}>Reject</Button>
          </Space>
        </Card>
      ))}
    </section>
  );
}
