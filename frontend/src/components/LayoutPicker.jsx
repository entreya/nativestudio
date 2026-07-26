import React from 'react';
import { Button, Popover, Typography } from 'antd';
import { LayoutOutlined } from '@ant-design/icons';

const { Text } = Typography;

// A Windows 11 "snap layout"-style picker: one button opens a small map of
// the actual on-screen regions (Folders/Code/AI side by side, Terminal as a
// full-width strip underneath, matching EditorPage's real layout), and
// clicking a region toggles it — replacing four separate icon buttons that
// were easy to mix up (the Code and Terminal glyphs read as near-identical
// at a glance).
function zoneStyle(active, extra) {
  return {
    position: 'absolute',
    borderRadius: 5,
    cursor: 'pointer',
    border: `1.5px solid ${active ? 'var(--studio-accent, #c15f3c)' : 'var(--studio-border, #d8d1c5)'}`,
    background: active ? 'color-mix(in srgb, var(--studio-accent, #c15f3c) 16%, transparent)' : 'transparent',
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    fontSize: 10.5,
    fontWeight: 600,
    lineHeight: 1.2,
    textAlign: 'center',
    color: active ? 'var(--studio-accent, #c15f3c)' : 'var(--studio-muted, #746b63)',
    transition: 'background 0.12s ease, border-color 0.12s ease, color 0.12s ease',
    userSelect: 'none',
    ...extra,
  };
}

function LayoutDiagram({ layout, onToggle }) {
  return (
    <div style={{ position: 'relative', width: 230, height: 148 }}>
      <div style={zoneStyle(layout.folders, { left: 0, top: 0, width: 48, height: 104 })} onClick={() => onToggle('folders')}>
        Folders
      </div>
      <div style={zoneStyle(layout.code, { left: 52, top: 0, width: 126, height: 104 })} onClick={() => onToggle('code')}>
        Code
      </div>
      <div style={zoneStyle(layout.ai, { left: 182, top: 0, width: 48, height: 104 })} onClick={() => onToggle('ai')}>
        AI
      </div>
      <div style={zoneStyle(layout.terminal, { left: 0, top: 108, width: 230, height: 40 })} onClick={() => onToggle('terminal')}>
        Terminal
      </div>
    </div>
  );
}

export default function LayoutPicker({ layout, onToggleLayout }) {
  if (!layout) return null;
  return (
    <Popover
      trigger="click"
      placement="bottomRight"
      title={<Text strong style={{ fontSize: 12 }}>Layout — click a region to show/hide it</Text>}
      content={<LayoutDiagram layout={layout} onToggle={onToggleLayout} />}
    >
      <Button type="text" size="small" icon={<LayoutOutlined />} aria-label="Choose layout" />
    </Popover>
  );
}
