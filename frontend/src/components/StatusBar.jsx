import React from 'react';
import { Button, Space, Tooltip, Progress } from 'antd';
import { BranchesOutlined, CloseOutlined, DatabaseOutlined, LoadingOutlined, MinusOutlined } from '@ant-design/icons';

export default function StatusBar({ indexStatus, scanMinimized, onScanMinimize, onScanExpand }) {
  const scanning = indexStatus.status === 'scanning' || indexStatus.status === 'running';
  const enriching = indexStatus.enrichmentStatus === 'queued' || indexStatus.enrichmentStatus === 'running';
  const busy = scanning || enriching;
  const completed = (indexStatus.processed || 0) + (indexStatus.skipped || 0);
  const percent = indexStatus.total ? Math.min(100, Math.round((completed / indexStatus.total) * 100)) : 0;
  const activity = scanning
    ? (indexStatus.total > 0 ? `Building code index ${completed}/${indexStatus.total}` : 'Scanning workspace…')
    : `Enriching AI knowledge${indexStatus.enrichmentRemaining > 0 ? ` · ${indexStatus.enrichmentRemaining} remaining` : '…'}`;

  return (
    <div className={`app-status-bar ${busy ? 'is-scanning' : ''}`}>
      <Space size="middle">
        <span className="status-item"><BranchesOutlined /> main</span>
        <span className="status-item"><CloseOutlined style={{ fontSize: 10 }} /> 0</span>
        {busy && (scanMinimized ? (
          <button type="button" className="scan-compact" onClick={onScanExpand} title="Show indexing progress">
            <LoadingOutlined spin /> {scanning ? 'Indexing code' : 'AI knowledge'} in background
          </button>
        ) : (
          <div className="scan-notification" role="status" aria-live="polite">
            <LoadingOutlined spin />
            <span>{activity}</span>
            {scanning && <Progress percent={percent} showInfo={false} size="small" className="scan-progress" />}
            <Button type="text" size="small" icon={<MinusOutlined />} onClick={onScanMinimize}>Run in background</Button>
          </div>
        ))}
        {indexStatus.status === 'completed_with_errors' && (
          <Tooltip title={`${indexStatus.errors || 0} indexing errors`}>
            <span className="status-item status-warning"><DatabaseOutlined /> Indexed with warnings</span>
          </Tooltip>
        )}
      </Space>
      <Space size="middle" className="status-right">
        <span>Ln 1, Col 1</span><span>UTF-8</span><span>Go</span><span>Prettier</span>
      </Space>
    </div>
  );
}
