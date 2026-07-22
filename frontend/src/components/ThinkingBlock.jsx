import React, { useState } from 'react';
import { Typography, theme } from 'antd';

const { Link } = Typography;
const { useToken } = theme;

export default function ThinkingBlock({ text, live = false }) {
  const [open, setOpen] = useState(true);
  const { token } = useToken();

  if (!text) return null;

  return (
    <div style={{ marginBottom: 8 }}>
      <Link 
        onClick={() => setOpen(!open)}
        style={{ fontStyle: 'italic', color: token.colorTextSecondary, fontSize: '12px' }}
      >
        {open ? `▲ ${live ? 'Thinking…' : 'Hide thinking'}` : '▼ Show thinking'}
      </Link>
      
      {open && (
        <div style={{ 
          marginTop: 8, 
          padding: 12, 
          backgroundColor: 'var(--studio-panel, #fafaf0)',
          border: '1px solid var(--studio-border, #e8e8d0)',
          borderRadius: 8,
          maxHeight: 300, 
          overflowY: 'auto',
          fontSize: '12.8px', 
          fontStyle: 'italic', 
          color: token.colorTextSecondary,
          whiteSpace: 'pre-wrap'
        }}>
          {text}
        </div>
      )}
    </div>
  );
}
