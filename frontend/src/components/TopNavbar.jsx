import React, { useEffect, useRef, useState } from 'react';
import { Button, Dropdown, Modal, Tooltip, Typography, Popover, Badge, Divider, message } from 'antd';
import {
  DatabaseOutlined,
  FileOutlined,
  FolderOpenOutlined,
  InfoCircleOutlined,
  MessageOutlined,
  QuestionCircleOutlined,
  CodeOutlined,
  RobotOutlined,
  BellOutlined,
  SettingOutlined,
  ThunderboltOutlined,
} from '@ant-design/icons';
import { useAppState } from '../state/useAppState';
import IndexActivityWidget from './IndexActivityWidget';

const { Text } = Typography;

export default function TopNavbar({ folderName, onNavigate, onOpenFile, onOpenFolder, layout, onToggleLayout }) {
  const { indexProcesses, stopIndexing, approveEnrichment } = useAppState();

  // Open the notification panel by default whenever there's something to
  // see — either on first load (work already in progress) or the moment new
  // work appears — without permanently pinning it open: once the user
  // dismisses it, it only pops open again for the *next* new notification.
  // Keyed off a primitive count (not the indexProcesses array itself, which
  // is recreated on every context render regardless of whether anything
  // actually changed) so the 0-to-N transition is detected reliably.
  const notifCount = indexProcesses?.length || 0;
  const [notifOpen, setNotifOpen] = useState(notifCount > 0);
  const prevCountRef = useRef(notifCount);
  useEffect(() => {
    if (notifCount > 0 && prevCountRef.current === 0) setNotifOpen(true);
    prevCountRef.current = notifCount;
  }, [notifCount]);

  const showHelp = () => Modal.info({
    title: 'NativeStudio Help',
    width: 520,
    content: (
      <div style={{ lineHeight: 1.7 }}>
        <p><strong>⌘/Ctrl + Enter</strong> — send a message</p>
        <p><strong>⌘/Ctrl + S</strong> — save the active file</p>
        <p>Open Conversations & Memory to revisit any project thread and its retained context.</p>
      </div>
    ),
  });
  const showAbout = () => Modal.info({
    title: 'About NativeStudio',
    content: 'A local-first AI code editor powered by Ollama, persistent conversations, and reviewable code changes.',
  });
  const fileItems = [
    { key: 'open-file', icon: <FileOutlined />, label: 'Open File…' },
    { key: 'open-folder', icon: <FolderOpenOutlined />, label: 'Open Folder…' },
    { type: 'divider' },
    { key: 'conversations', icon: <MessageOutlined />, label: 'Conversations & Memory' },
    { key: 'knowledge', icon: <DatabaseOutlined />, label: 'Project Knowledge' },
    { key: 'database', icon: <DatabaseOutlined />, label: 'Database Explorer' },
    { type: 'divider' },
    { key: 'settings', icon: <SettingOutlined />, label: 'Settings (Themes & Fonts)' },
  ];
  const helpItems = [
    { key: 'help', icon: <QuestionCircleOutlined />, label: 'Help' },
    { key: 'about', icon: <InfoCircleOutlined />, label: 'About NativeStudio' },
  ];
  const handleFileMenu = ({ key }) => {
    if (key === 'open-file') onOpenFile?.();
    if (key === 'open-folder') onOpenFolder?.();
    if (key === 'conversations') onNavigate('conversations');
    if (key === 'knowledge') onNavigate('knowledge');
    if (key === 'database') onNavigate('db');
    if (key === 'settings') onNavigate('settings');
  };
  const handleHelpMenu = ({ key }) => {
    if (key === 'help') showHelp();
    if (key === 'about') showAbout();
  };

  // Ollama's own idle timeout (or our shortened one) frees memory eventually,
  // but holding a 7GB+ model resident on a 16GB machine causes real, felt
  // slowdown/heat in the meantime — this lets the user reclaim it instantly
  // instead of waiting.
  const [unloading, setUnloading] = useState(false);
  const handleFreeMemory = async () => {
    setUnloading(true);
    try {
      const res = await fetch('/api/models/unload', { method: 'POST' });
      const data = await res.json();
      if (data.ok && data.unloaded?.length > 0) {
        message.success(`Freed memory from ${data.unloaded.length} model${data.unloaded.length === 1 ? '' : 's'}`);
      } else {
        message.info('No models were loaded');
      }
    } catch {
      message.error('Could not reach Ollama');
    } finally {
      setUnloading(false);
    }
  };

  return (
    <header className="claude-navbar">
      {/* Left: App Menus */}
      <nav className="claude-app-menus" aria-label="Application menu">
        <Dropdown menu={{ items: fileItems, onClick: handleFileMenu }} trigger={['click']} placement="bottomLeft">
          <Button type="text" style={{ fontWeight: 500 }}>File</Button>
        </Dropdown>
        <Dropdown menu={{ items: helpItems, onClick: handleHelpMenu }} trigger={['click']} placement="bottomLeft">
          <Button type="text" style={{ fontWeight: 500 }}>Help</Button>
        </Dropdown>
      </nav>

      {/* Center: Project/Folder Name */}
      <div className="claude-nav-center">
        <div className="claude-nav-folder">
          {folderName ? (
            <><FolderOpenOutlined style={{ color: 'var(--studio-accent, #c15f3c)' }} /><Text strong>{folderName}</Text></>
          ) : (
            <><DatabaseOutlined style={{ color: 'var(--studio-accent, #c15f3c)' }} /><Text strong>All projects</Text></>
          )}
        </div>
      </div>

      {/* Right: Layout Switcher and Notifications */}
      <div className="claude-nav-right">
        {layout && (
          <div className="layout-switcher" aria-label="Layout visibility">
            {[
              { key: 'folders', label: 'Folders', icon: <FolderOpenOutlined /> },
              { key: 'code', label: 'Code', icon: <CodeOutlined /> },
              { key: 'ai', label: 'AI', icon: <RobotOutlined /> },
            ].map(item => (
              <Tooltip key={item.key} title={`${layout[item.key] ? 'Hide' : 'Show'} ${item.label}`}>
                <Button
                  type="text"
                  size="small"
                  className={layout[item.key] ? 'is-visible' : ''}
                  aria-label={`${layout[item.key] ? 'Hide' : 'Show'} ${item.label}`}
                  aria-pressed={layout[item.key]}
                  icon={item.icon}
                  onClick={() => onToggleLayout?.(item.key)}
                />
              </Tooltip>
            ))}
          </div>
        )}
        
        <div>
          <Popover
            placement="bottomRight"
            title={<span style={{ fontWeight: 600 }}>Notifications</span>}
            content={
              <div>
                {indexProcesses && indexProcesses.length > 0
                  ? <IndexActivityWidget processes={indexProcesses} onStop={stopIndexing} onApprove={approveEnrichment} />
                  : <Text type="secondary" style={{ padding: '8px 4px' }}>Nothing to report</Text>}
                <Divider style={{ margin: '10px 0' }} />
                <Button type="text" size="small" icon={<ThunderboltOutlined />} loading={unloading} onClick={handleFreeMemory} style={{ width: '100%', justifyContent: 'flex-start', color: 'var(--studio-muted, #746b63)' }}>
                  Free up memory
                </Button>
              </div>
            }
            trigger="click"
            open={notifOpen}
            onOpenChange={setNotifOpen}
          >
            <Badge count={indexProcesses?.filter(p => p.status === 'running' || p.status === 'pending_confirmation').length || 0} size="small">
              <Button type="text" icon={<BellOutlined />} style={{ borderRadius: '50%' }} />
            </Badge>
          </Popover>
        </div>
      </div>
    </header>
  );
}
