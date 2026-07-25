import React, { useState } from 'react';
import { Alert, Button, Card, Space, Tag, Typography } from 'antd';
import { CheckOutlined, CloseOutlined, CodeOutlined } from '@ant-design/icons';

const { Text } = Typography;

/**
 * Staged shell commands awaiting explicit approval before they actually run
 * — the same review-before-acting pattern as PatchReview, for the one tool
 * (run_command) that isn't confined to the workspace the way file edits are.
 */
export default function CommandReview({ commands, onApprove, onReject, onApproveAll, onRejectAll }) {
  const [resolving, setResolving] = useState([]);
  const [error, setError] = useState('');
  const run = async (ids, action) => {
    setError('');
    setResolving(current => [...new Set([...current, ...ids])]);
    try {
      await action();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Could not resolve the staged command.');
    } finally {
      setResolving(current => current.filter(id => !ids.includes(id)));
    }
  };

  if (!commands?.length) return null;
  const pending = commands.filter(cmd => !cmd.status || cmd.status === 'pending');
  const pendingIDs = pending.map(cmd => cmd.run_id);
  const statusTag = { completed: ['success', 'DONE'], failed: ['error', 'FAILED'] };
  return (
    <section aria-label="Staged commands" style={{ width: '100%', display: 'flex', flexDirection: 'column', gap: 10 }}>
      {error && <Alert type="error" showIcon message={error} closable onClose={() => setError('')} />}
      {pending.length > 0 && (
        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 8, flexWrap: 'wrap' }}>
          <Text strong>{pending.length} command{pending.length === 1 ? '' : 's'} to run — review before executing</Text>
          {pending.length > 1 && (
            <Space size={6}>
              <Button size="small" type="primary" icon={<CheckOutlined />} loading={resolving.length > 0} onClick={() => run(pendingIDs, onApproveAll)}>Approve All</Button>
              <Button size="small" danger icon={<CloseOutlined />} disabled={resolving.length > 0} onClick={() => run(pendingIDs, onRejectAll)}>Reject All</Button>
            </Space>
          )}
        </div>
      )}
      {commands.map(cmd => {
        const isPending = !cmd.status || cmd.status === 'pending';
        const [tagColor, tagLabel] = statusTag[cmd.status] || ['warning', 'SHELL'];
        return (
          <Card
            key={cmd.run_id}
            size="small"
            title={<Space size={7}><CodeOutlined /><Text ellipsis style={{ maxWidth: 360 }} title={cmd.cwd}>{cmd.cwd && cmd.cwd !== '.' ? cmd.cwd : 'workspace root'}</Text><Tag color={tagColor}>{tagLabel}</Tag></Space>}
            styles={{ body: { padding: 10 } }}
          >
            <pre style={{ margin: 0, padding: 10, borderRadius: 6, background: 'var(--studio-panel, #f5f1e9)', fontSize: 12.5, fontFamily: 'monospace', whiteSpace: 'pre-wrap', overflowX: 'auto' }}>
              {cmd.command}
            </pre>
            {cmd.output && (
              <pre style={{ margin: '8px 0 0', padding: 10, borderRadius: 6, maxHeight: 240, overflow: 'auto', fontSize: 11.5, whiteSpace: 'pre-wrap', background: cmd.status === 'failed' ? 'color-mix(in srgb, var(--studio-danger, #b85c5c) 10%, transparent)' : 'var(--studio-bg, #e2e8f0)', color: cmd.status === 'failed' ? 'var(--studio-danger, #b85c5c)' : 'inherit' }}>
                {cmd.output}
              </pre>
            )}
            {isPending && (
              <Space size={7} style={{ display: 'flex', justifyContent: 'flex-end', marginTop: 10 }}>
                <Button size="small" type="primary" icon={<CheckOutlined />} loading={resolving.includes(cmd.run_id)} onClick={() => run([cmd.run_id], () => onApprove(cmd.run_id))}>Run</Button>
                <Button size="small" danger icon={<CloseOutlined />} disabled={resolving.includes(cmd.run_id)} onClick={() => run([cmd.run_id], () => onReject(cmd.run_id))}>Reject</Button>
              </Space>
            )}
          </Card>
        );
      })}
    </section>
  );
}
