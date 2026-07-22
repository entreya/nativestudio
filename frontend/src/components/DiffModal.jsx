import React, { useState } from 'react';
import { Modal, Button, Typography, Checkbox, Space } from 'antd';

const { Title, Text } = Typography;

// Above this many (origLines * suggLines) cells, the O(m*n) LCS matrix below
// gets big enough (tens of millions of numbers, allocated synchronously on
// the main thread) to freeze the tab. Past that size we skip the fine-grained
// diff and fall back to a coarse whole-block replacement, which is still
// correct — just not line-matched.
const MAX_DIFF_CELLS = 4_000_000;

function computeDiff(orig, sugg) {
  const origLines = orig.split('\n');
  const suggLines = sugg.split('\n');
  const m = origLines.length;
  const n = suggLines.length;

  if (m * n > MAX_DIFF_CELLS) {
    const diff = [
      ...origLines.map(content => ({ type: 'delete', content, checked: true })),
      ...suggLines.map(content => ({ type: 'insert', content, checked: true })),
    ];
    return {
      diff,
      stats: { inserts: suggLines.length, deletes: origLines.length },
      coarse: true,
    };
  }

  const dp = Array(m + 1).fill(null).map(() => Array(n + 1).fill(0));

  for (let i = 1; i <= m; i++) {
    for (let j = 1; j <= n; j++) {
      if (origLines[i - 1] === suggLines[j - 1]) dp[i][j] = dp[i - 1][j - 1] + 1;
      else dp[i][j] = Math.max(dp[i - 1][j], dp[i][j - 1]);
    }
  }

  let i = m;
  let j = n;
  const diff = [];
  while (i > 0 || j > 0) {
    if (i > 0 && j > 0 && origLines[i - 1] === suggLines[j - 1]) {
      diff.push({ type: 'equal', content: origLines[i - 1] });
      i--; j--;
    } else if (j > 0 && (i === 0 || dp[i][j - 1] >= dp[i - 1][j])) {
      diff.push({ type: 'insert', content: suggLines[j - 1], checked: true });
      j--;
    } else {
      diff.push({ type: 'delete', content: origLines[i - 1], checked: true });
      i--;
    }
  }
  diff.reverse();
  return {
    diff,
    stats: {
      inserts: diff.filter(line => line.type === 'insert').length,
      deletes: diff.filter(line => line.type === 'delete').length,
    },
    coarse: false,
  };
}

export default function DiffModal({ open, onReject, originalCode, suggestedCode, onApply }) {
  const initialDiff = computeDiff(originalCode || '', suggestedCode || '');
  const [diffLines, setDiffLines] = useState(initialDiff.diff);
  const stats = initialDiff.stats;
  const coarse = initialDiff.coarse;

  const handleToggle = (index) => {
    const newDiff = [...diffLines];
    newDiff[index].checked = !newDiff[index].checked;
    setDiffLines(newDiff);
  };

  const handleAcceptAll = () => {
    const newDiff = diffLines.map(d => (d.type !== 'equal' ? { ...d, checked: true } : d));
    setDiffLines(newDiff);
  };

  const handleRejectAll = () => {
    const newDiff = diffLines.map(d => (d.type !== 'equal' ? { ...d, checked: false } : d));
    setDiffLines(newDiff);
  };

  const handleApply = () => {
    const result = [];
    diffLines.forEach(line => {
      if (line.type === 'equal') {
        result.push(line.content);
      } else if (line.type === 'delete') {
        if (!line.checked) result.push(line.content);
      } else if (line.type === 'insert') {
        if (line.checked) result.push(line.content);
      }
    });
    onApply(result.join('\n'));
  };

  let origLineNum = 1;
  let suggLineNum = 1;

  const header = (
    <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', width: '100%', paddingRight: 24 }}>
      <Title level={5} style={{ margin: 0 }}>AI Suggested Edit</Title>
      <Space>
        <Button onClick={handleAcceptAll} size="small">Accept All</Button>
        <Button onClick={handleRejectAll} size="small">Reject All</Button>
      </Space>
    </div>
  );

  const footer = (
    <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
      <Text type="secondary">
        +{stats.inserts} -{stats.deletes} lines
        {coarse && ' · large file: showing a simplified whole-file diff'}
      </Text>
      <Space>
        <Button onClick={onReject}>Reject Changes</Button>
        <Button onClick={handleApply} type="primary">Accept Changes</Button>
      </Space>
    </div>
  );

  return (
    <Modal 
      open={open} 
      onCancel={onReject} 
      title={header}
      footer={footer}
      width="90vw"
      style={{ top: 20 }}
      styles={{ 
        body: { 
          height: '75vh', 
          display: 'flex', 
          padding: 0, 
          fontFamily: 'monospace', 
          fontSize: '13px',
          borderTop: '1px solid var(--studio-border, #f0f0f0)',
          borderBottom: '1px solid var(--studio-border, #f0f0f0)'
        } 
      }}
      closeIcon={null}
    >
      <div style={{ flex: 1, overflowX: 'auto', overflowY: 'auto', borderRight: '1px solid #ccc' }}>
        {diffLines.map((line, idx) => {
          if (line.type === 'insert') {
            return <div key={idx} style={{ display: 'flex', backgroundColor: 'var(--studio-panel, #f5f5f5)', color: 'transparent' }}><div style={{ width: '40px' }}/>&nbsp;</div>;
          }
          const bg = line.type === 'delete' ? '#fef2f2' : 'transparent';
          const color = line.type === 'delete' ? '#991b1b' : 'inherit';
          const opacity = (line.type === 'delete' && !line.checked) ? 0.5 : 1;
          const td = (line.type === 'delete' && !line.checked) ? 'line-through' : 'none';
          
          return (
            <div 
              key={idx} 
              className="diff-row"
              style={{ display: 'flex', backgroundColor: bg, color, opacity, textDecoration: td, position: 'relative' }}
            >
              <div style={{ width: '40px', borderRight: '1px solid #ccc', textAlign: 'right', paddingRight: 8, color: '#888', userSelect: 'none', flexShrink: 0 }}>
                {origLineNum++}
              </div>
              {line.type === 'delete' && (
                <Checkbox 
                  className="diff-cb" 
                  checked={line.checked} 
                  onChange={() => handleToggle(idx)} 
                  style={{ position: 'absolute', marginLeft: '45px', zIndex: 10, opacity: 0 }} 
                />
              )}
              <div style={{ paddingLeft: 32, paddingRight: 8, whiteSpace: 'pre' }}>{line.content || ' '}</div>
              <style>{`.diff-row:hover .diff-cb { opacity: 1 !important; }`}</style>
            </div>
          );
        })}
      </div>
      <div style={{ flex: 1, overflowX: 'auto', overflowY: 'auto' }}>
        {diffLines.map((line, idx) => {
          if (line.type === 'delete') {
            return <div key={idx} style={{ display: 'flex', backgroundColor: 'var(--studio-panel, #f5f5f5)', color: 'transparent' }}><div style={{ width: '40px' }}/>&nbsp;</div>;
          }
          const bg = line.type === 'insert' ? '#f0fdf4' : 'transparent';
          const color = line.type === 'insert' ? '#166534' : 'inherit';
          const opacity = (line.type === 'insert' && !line.checked) ? 0.5 : 1;
          const td = (line.type === 'insert' && !line.checked) ? 'line-through' : 'none';
          return (
            <div 
              key={idx} 
              className="diff-row"
              style={{ display: 'flex', backgroundColor: bg, color, opacity, textDecoration: td, position: 'relative' }}
            >
              <div style={{ width: '40px', borderRight: '1px solid #ccc', textAlign: 'right', paddingRight: 8, color: '#888', userSelect: 'none', flexShrink: 0 }}>
                {suggLineNum++}
              </div>
              {line.type === 'insert' && (
                <Checkbox 
                  className="diff-cb" 
                  checked={line.checked} 
                  onChange={() => handleToggle(idx)} 
                  style={{ position: 'absolute', marginLeft: '45px', zIndex: 10, opacity: 0 }} 
                />
              )}
              <div style={{ paddingLeft: 32, paddingRight: 8, whiteSpace: 'pre' }}>{line.content || ' '}</div>
            </div>
          );
        })}
      </div>
    </Modal>
  );
}
