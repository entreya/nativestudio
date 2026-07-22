import React, { useState } from 'react';
import { Typography, theme } from 'antd';
import {
  ToolOutlined,
  CheckCircleOutlined,
  CloseCircleOutlined,
  DownOutlined,
  RightOutlined
} from '@ant-design/icons';

const { Text } = Typography;
const { useToken } = theme;

export default function ToolCallCard({ toolCall }) {
  const [expanded, setExpanded] = useState(false);
  const { token } = useToken();
  
  const status = toolCall.status || 'running';
  
  let Icon = ToolOutlined;
  let color = token.colorTextSecondary;
  let bgColor = 'rgba(148, 163, 184, 0.1)';
  
  if (status === 'done') {
    Icon = CheckCircleOutlined;
    color = token.colorSuccess;
    bgColor = 'rgba(34, 197, 94, 0.1)';
  } else if (status === 'error') {
    Icon = CloseCircleOutlined;
    color = token.colorError;
    bgColor = 'rgba(239, 68, 68, 0.1)';
  }

  // Generate a short preview of the input
  let preview = '';
  if (toolCall.input) {
    if (toolCall.input.path) preview = toolCall.input.path;
    else if (toolCall.input.query) preview = `"${toolCall.input.query}"`;
    else preview = '{...}';
  }

  return (
    <div style={{ marginBottom: 8, borderRadius: 8, overflow: 'hidden', border: `1px solid ${token.colorBorderSecondary}` }}>
      <div 
        onClick={() => setExpanded(!expanded)}
        style={{ 
          display: 'flex', alignItems: 'center', padding: 8, gap: 8, 
          backgroundColor: bgColor, cursor: 'pointer'
        }}
        onMouseOver={(e) => e.currentTarget.style.filter = 'brightness(0.95)'}
        onMouseOut={(e) => e.currentTarget.style.filter = 'brightness(1)'}
      >
        <Icon style={{ fontSize: 16, color }} className={status === 'running' ? 'anticon-spin' : ''} />
        <Text style={{ fontWeight: 600, fontFamily: 'monospace', fontSize: '12.8px' }}>
          {toolCall.name}
        </Text>
        <Text type="secondary" style={{ marginLeft: 8, flexGrow: 1, whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis', fontSize: '12px' }}>
          {preview}
        </Text>
        {expanded ? <DownOutlined style={{ fontSize: 14, color: token.colorTextSecondary }} /> : <RightOutlined style={{ fontSize: 14, color: token.colorTextSecondary }} />}
      </div>
      
      {expanded && (
        <div style={{ padding: 12, backgroundColor: 'var(--studio-panel, #f8fafc)', display: 'flex', flexDirection: 'column', gap: 8 }}>
          <div>
            <Text type="secondary" strong style={{ fontSize: '12px' }}>Input:</Text>
            <pre style={{ margin: 0, marginTop: 4, padding: 8, backgroundColor: 'var(--studio-bg, #e2e8f0)', borderRadius: 4, fontSize: '12px', overflowX: 'auto', whiteSpace: 'pre-wrap' }}>
              {JSON.stringify(toolCall.input, null, 2)}
            </pre>
          </div>
          
          {(toolCall.output || toolCall.error) && (
            <div>
              <Text type="secondary" strong style={{ fontSize: '12px' }}>
                {status === 'error' ? 'Error:' : 'Output:'}
              </Text>
              <pre style={{ 
                margin: 0, marginTop: 4, padding: 8, borderRadius: 4, fontSize: '12px', overflowX: 'auto', whiteSpace: 'pre-wrap',
                backgroundColor: status === 'error' ? 'rgba(239, 68, 68, 0.1)' : 'var(--studio-bg, #e2e8f0)',
                color: status === 'error' ? token.colorErrorText : 'inherit'
              }}>
                {toolCall.error || (typeof toolCall.output === 'string' ? toolCall.output : JSON.stringify(toolCall.output, null, 2))}
              </pre>
            </div>
          )}
        </div>
      )}
    </div>
  );
}
