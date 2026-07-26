import React, { useState } from 'react';
import { Alert, Button, Card, Space, Tag, Typography } from 'antd';
import { CheckOutlined, UndoOutlined, FileOutlined } from '@ant-design/icons';
import { DiffView } from './PatchReview';

const { Text } = Typography;

// Shows file changes a run_terminal command already wrote to disk directly
// (no staging step, unlike create_file/apply_patch) — Keep just dismisses
// the notice, Undo calls the same rollback endpoint an applied create_file/
// apply_patch change already uses to revert.
export default function AppliedPatchReview({ patches, onKeep, onUndo }) {
  const [resolving, setResolving] = useState([]);
  const [error, setError] = useState('');
  const run = async (id, action) => {
    setError('');
    setResolving(current => [...current, id]);
    try {
      await action();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Could not resolve this change.');
    } finally {
      setResolving(current => current.filter(item => item !== id));
    }
  };

  if (!patches?.length) return null;
  return (
    <section aria-label="Applied terminal changes" style={{ width: '100%', display: 'flex', flexDirection: 'column', gap: 10 }}>
      {error && <Alert type="error" showIcon message={error} closable onClose={() => setError('')} />}
      <Text strong>{patches.length} file{patches.length === 1 ? '' : 's'} changed by a terminal command</Text>
      {patches.map(patch => (
        <Card
          key={patch.patch_id}
          size="small"
          title={<Space size={7}><FileOutlined /><Text ellipsis style={{ maxWidth: 420 }}>{patch.file_path}</Text><Tag color={patch.operation === 'delete' ? 'error' : patch.operation === 'create' ? 'success' : 'processing'}>{patch.operation.toUpperCase()}</Tag></Space>}
          styles={{ body: { padding: 10 } }}
        >
          <DiffView diff={patch.diff} />
          <Space size={7} style={{ display: 'flex', justifyContent: 'flex-end', marginTop: 10 }}>
            <Button size="small" type="primary" icon={<CheckOutlined />} loading={resolving.includes(patch.patch_id)} onClick={() => run(patch.patch_id, () => onKeep(patch.patch_id))}>Keep</Button>
            <Button size="small" danger icon={<UndoOutlined />} disabled={resolving.includes(patch.patch_id)} onClick={() => run(patch.patch_id, () => onUndo(patch.patch_id))}>Undo</Button>
          </Space>
        </Card>
      ))}
    </section>
  );
}
