import React from 'react';
import ConversationsPage from '../components/ConversationsPage';
import TopNavbar from '../components/TopNavbar';
import { useAppState } from '../state/useAppState';

export default function ConversationsRoute() {
  const { studioStyle, studioClassName, handleNavigate, handleOpenFile, handleCreateProject, handleOpenConversation } = useAppState();

  return (
    <div className={studioClassName} style={{ ...studioStyle, display: 'flex', flexDirection: 'column', width: '100vw', height: '100vh', overflow: 'hidden', background: 'var(--studio-bg, #f7f4ed)' }}>
      <TopNavbar folderName="" onNavigate={handleNavigate} onOpenFile={handleOpenFile} onOpenFolder={handleCreateProject} />
      <ConversationsPage onOpenConversation={handleOpenConversation} />
    </div>
  );
}
