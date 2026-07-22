import React from 'react';
import { CheckCircleFilled, CloseCircleFilled, LoadingOutlined, SearchOutlined } from '@ant-design/icons';
import { Typography } from 'antd';
import ToolCallCard from './ToolCallCard';

const { Text } = Typography;

export default function AgentTimeline({ activities = [], running = false }) {
  if (activities.length === 0) return null;

  return (
    <div style={{ margin: '8px 0 12px', paddingLeft: 4 }} aria-label="Agent activity timeline">
      {activities.map((activity, index) => {
        const isLast = index === activities.length - 1;
        const status = activity.status || (running && isLast ? 'running' : 'done');
        const Icon = status === 'error' ? CloseCircleFilled
          : status === 'running' ? LoadingOutlined
          : activity.kind === 'context' ? SearchOutlined
          : CheckCircleFilled;
        const color = status === 'error' ? '#b85c5c' : status === 'running' ? 'var(--studio-accent, #c15f3c)' : '#6b8f71';

        return (
          <div key={activity.id || `${activity.kind}-${index}`} style={{ display: 'grid', gridTemplateColumns: '18px 1fr', gap: 8 }}>
            <div style={{ position: 'relative', display: 'flex', justifyContent: 'center' }}>
              {!isLast && <span style={{ position: 'absolute', top: 16, bottom: -5, width: 1, background: 'var(--studio-border, #d8d1c5)' }} />}
              <Icon spin={status === 'running'} style={{ position: 'relative', zIndex: 1, marginTop: 2, color, fontSize: 12, background: 'var(--studio-bg, #f7f4ed)' }} />
            </div>
            <div style={{ minWidth: 0, paddingBottom: isLast ? 0 : 9 }}>
              <Text style={{ color: 'var(--studio-muted, #746b63)', fontSize: 11 }}>{activity.label}</Text>
              {activity.toolCall && <div style={{ marginTop: 5 }}><ToolCallCard toolCall={activity.toolCall} /></div>}
            </div>
          </div>
        );
      })}
    </div>
  );
}
