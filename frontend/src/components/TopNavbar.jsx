import React from 'react';
import { Button, Dropdown, Modal, Tooltip, Typography } from 'antd';
import {
  DatabaseOutlined,
  FileOutlined,
  FolderOpenOutlined,
  InfoCircleOutlined,
  MessageOutlined,
  QuestionCircleOutlined,
  CodeOutlined,
  RobotOutlined,
} from '@ant-design/icons';

const { Text } = Typography;

export default function TopNavbar({ folderName, onNavigate, onOpenFile, onOpenFolder, layout, onToggleLayout }) {
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
  };
  const handleHelpMenu = ({ key }) => {
    if (key === 'help') showHelp();
    if (key === 'about') showAbout();
  };

  return (
    <header className="claude-navbar">
      <nav className="claude-app-menus" aria-label="Application menu">
        <Dropdown menu={{ items: fileItems, onClick: handleFileMenu }} trigger={['click']} placement="bottomLeft">
          <Button type="text" size="small">File</Button>
        </Dropdown>
        <Dropdown menu={{ items: helpItems, onClick: handleHelpMenu }} trigger={['click']} placement="bottomLeft">
          <Button type="text" size="small">Help</Button>
        </Dropdown>
      </nav>

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
        <div className="claude-nav-folder">
          {folderName ? <><FolderOpenOutlined /><Text>{folderName}</Text></> : <><DatabaseOutlined /><Text>All projects</Text></>}
        </div>
      </div>
    </header>
  );
}
