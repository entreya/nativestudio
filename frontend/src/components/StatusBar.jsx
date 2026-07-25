import React from 'react';
import { Space, Tooltip } from 'antd';
import { BranchesOutlined, CloseOutlined, DatabaseOutlined } from '@ant-design/icons';

// Map file extensions to human-readable language names for the status bar.
const LANG_LABELS = {
  '.js': 'JavaScript', '.jsx': 'JavaScript',
  '.ts': 'TypeScript', '.tsx': 'TypeScript (React)',
  '.go': 'Go', '.py': 'Python', '.php': 'PHP',
  '.json': 'JSON', '.md': 'Markdown',
  '.html': 'HTML', '.css': 'CSS', '.scss': 'SCSS',
  '.yml': 'YAML', '.yaml': 'YAML',
  '.sh': 'Shell', '.bash': 'Shell',
  '.rs': 'Rust', '.c': 'C', '.cpp': 'C++', '.h': 'C/C++',
  '.java': 'Java', '.kt': 'Kotlin', '.swift': 'Swift',
  '.rb': 'Ruby', '.lua': 'Lua', '.sql': 'SQL',
};

function langFromPath(path) {
  if (!path) return '';
  const dot = path.lastIndexOf('.');
  if (dot === -1) return 'Plain Text';
  return LANG_LABELS[path.slice(dot)] || 'Plain Text';
}

export default function StatusBar({
  indexStatus,
  cursor,
  activeTab,
  selectionLength,
}) {
  const line = cursor?.line ?? 1;
  const col = cursor?.column ?? 1;
  const lang = langFromPath(activeTab);

  return (
    <div className="app-status-bar">
      <Space size="middle">
        <span className="status-item"><BranchesOutlined /> main</span>
        <span className="status-item"><CloseOutlined style={{ fontSize: 10 }} /> 0</span>
        {indexStatus.status === 'completed_with_errors' && (
          <Tooltip title={`${indexStatus.errors || 0} indexing errors`}>
            <span className="status-item status-warning"><DatabaseOutlined /> Indexed with warnings</span>
          </Tooltip>
        )}
      </Space>
      <Space size="middle" className="status-right">
        <span>Ln {line}, Col {col}</span>
        {selectionLength > 0 && <span>{selectionLength.toLocaleString()} selected</span>}
        <span>UTF-8</span>
        {lang && <span>{lang}</span>}
      </Space>
    </div>
  );
}
