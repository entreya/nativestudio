import React, { useEffect, useRef, useState } from 'react';
import { Button, Dropdown, Modal, Tooltip, Typography, Popover, Badge, Divider, message } from 'antd';
import {
  DatabaseOutlined,
  FileOutlined,
  FolderOpenOutlined,
  InfoCircleOutlined,
  MessageOutlined,
  QuestionCircleOutlined,
  HomeOutlined,
  BulbOutlined,
  BellOutlined,
  SettingOutlined,
  ThunderboltOutlined,
} from '@ant-design/icons';
import { useAppState } from '../state/useAppState';
import IndexActivityWidget from './IndexActivityWidget';
import LayoutPicker from './LayoutPicker';

const { Text } = Typography;

export default function TopNavbar({ folderName, onNavigate, onOpenFile, onOpenFolder, layout, onToggleLayout }) {
  const { indexProcesses, stopIndexing, approveEnrichment, declineEnrichment, pauseEnrichment, resumeEnrichment } = useAppState();

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
  // Grouped by what the item does rather than one flat "File" list holding
  // opening files, navigating to three different pages, and settings all at
  // once. Splitting them means each menu answers one question.
  const fileItems = [
    { key: 'open-file', icon: <FileOutlined />, label: 'Open File…' },
    { key: 'open-folder', icon: <FolderOpenOutlined />, label: 'Open Folder…' },
    { type: 'divider' },
    { key: 'home', icon: <HomeOutlined />, label: 'Back to Projects' },
  ];
  const viewItems = [
    { key: 'conversations', icon: <MessageOutlined />, label: 'Conversations & Memory' },
    { key: 'knowledge', icon: <BulbOutlined />, label: 'Project Knowledge' },
    { key: 'database', icon: <DatabaseOutlined />, label: 'Database Explorer' },
  ];
  const toolItems = [
    { key: 'free-memory', icon: <ThunderboltOutlined />, label: 'Free Up Memory' },
    { type: 'divider' },
    { key: 'settings', icon: <SettingOutlined />, label: 'Settings…' },
  ];
  const helpItems = [
    { key: 'help', icon: <QuestionCircleOutlined />, label: 'Keyboard Shortcuts' },
    { key: 'about', icon: <InfoCircleOutlined />, label: 'About NativeStudio' },
  ];

  const handleFileMenu = ({ key }) => {
    if (key === 'open-file') onOpenFile?.();
    if (key === 'open-folder') onOpenFolder?.();
    if (key === 'home') onNavigate('home');
  };
  const handleViewMenu = ({ key }) => onNavigate(key === 'database' ? 'db' : key);
  const handleToolsMenu = ({ key }) => {
    if (key === 'settings') onNavigate('settings');
    if (key === 'free-memory') handleFreeMemory();
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
          <Button type="text" size="small" style={{ fontWeight: 500 }}>File</Button>
        </Dropdown>
        <Dropdown menu={{ items: viewItems, onClick: handleViewMenu }} trigger={['click']} placement="bottomLeft">
          <Button type="text" size="small" style={{ fontWeight: 500 }}>View</Button>
        </Dropdown>
        <Dropdown menu={{ items: toolItems, onClick: handleToolsMenu }} trigger={['click']} placement="bottomLeft">
          <Button type="text" size="small" style={{ fontWeight: 500 }}>Tools</Button>
        </Dropdown>
        <Dropdown menu={{ items: helpItems, onClick: handleHelpMenu }} trigger={['click']} placement="bottomLeft">
          <Button type="text" size="small" style={{ fontWeight: 500 }}>Help</Button>
        </Dropdown>
      </nav>

      {/* Center: Project/Folder Name — click to return to the home workspace */}
      <div className="claude-nav-center">
        <Tooltip title="Back to home workspace">
          <div className="claude-nav-folder claude-nav-folder-clickable" onClick={() => onNavigate('home')} role="button" tabIndex={0}>
            {folderName ? (
              <><FolderOpenOutlined style={{ color: 'var(--studio-accent, #c15f3c)' }} /><Text strong>{folderName}</Text></>
            ) : (
              <><DatabaseOutlined style={{ color: 'var(--studio-accent, #c15f3c)' }} /><Text strong>All projects</Text></>
            )}
          </div>
        </Tooltip>
      </div>

      {/* Right: Layout Picker and Notifications */}
      <div className="claude-nav-right">
        <LayoutPicker layout={layout} onToggleLayout={onToggleLayout} />

        <div>
          <Popover
            placement="bottomRight"
            title={<span style={{ fontWeight: 600 }}>Notifications</span>}
            content={
              <div>
                {indexProcesses && indexProcesses.length > 0
                  ? <IndexActivityWidget processes={indexProcesses} onStop={stopIndexing} onApprove={approveEnrichment} onDecline={declineEnrichment} onPause={pauseEnrichment} onResume={resumeEnrichment} />
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
            <Badge count={indexProcesses?.filter(p => p.status === 'running' || p.status === 'pending_confirmation' || p.status === 'paused').length || 0} size="small">
              <Button type="text" icon={<BellOutlined />} style={{ borderRadius: '50%' }} />
            </Badge>
          </Popover>
        </div>
      </div>
    </header>
  );
}
