import React from 'react';
import { useNavigate } from 'react-router-dom';
import TopNavbar from '../components/TopNavbar';
import ProjectKnowledgePage from '../components/ProjectKnowledgePage';
import StatusBar from '../components/StatusBar';
import { useAppState } from '../state/useAppState';
import { folderNameFromPath } from '../state/folderNameFromPath';
import { useProjectRouteSync } from '../hooks/useRouteSync';

export default function KnowledgePage() {
  const navigate = useNavigate();
  const projectId = useProjectRouteSync();
  const {
    studioStyle, studioClassName, activeProject,
    handleNavigate, handleOpenFile, handleCreateProject,
    setIndexStatus, setScanNotificationMinimized,
    indexStatus, scanNotificationMinimized,
    openFile,
  } = useAppState();

  if (!activeProject || activeProject.id !== projectId) return null;

  return (
    <div className={studioClassName} style={{ ...studioStyle, display: 'flex', flexDirection: 'column', width: '100vw', height: '100vh', overflow: 'hidden', background: 'var(--studio-bg, #f7f4ed)' }}>
      <TopNavbar folderName={folderNameFromPath(activeProject.path)} onNavigate={handleNavigate} onOpenFile={handleOpenFile} onOpenFolder={handleCreateProject} />
      <ProjectKnowledgePage
        project={activeProject}
        onBack={() => navigate(`/projects/${activeProject.id}`)}
        onRelearnStart={() => {
          setIndexStatus({ status: 'scanning', processed: 0, total: 0, skipped: 0, errors: 0 });
          setScanNotificationMinimized(false);
        }}
        onOpenFile={(path, source) => {
          openFile(`/${path.replace(/^\/+/, '')}`);
          if (source?.start_line) requestAnimationFrame(() => requestAnimationFrame(() => {
            window.editor?.revealLineInCenter(source.start_line);
            window.editor?.setSelection({ startLineNumber: source.start_line, startColumn: 1, endLineNumber: source.end_line || source.start_line, endColumn: 1 });
          }));
        }}
      />
      <StatusBar indexStatus={indexStatus} scanMinimized={scanNotificationMinimized} onScanMinimize={() => setScanNotificationMinimized(true)} onScanExpand={() => setScanNotificationMinimized(false)} />
    </div>
  );
}
