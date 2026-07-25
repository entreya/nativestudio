import React, { Suspense, lazy } from 'react';
import { Routes, Route, Navigate } from 'react-router-dom';
import { ConfigProvider, App as AntApp } from 'antd';
import WorkspacePickerModal from './components/WorkspacePickerModal';
import { AppStateProvider } from './state/AppStateContext';
import { useAppState } from './state/useAppState';
import { LandingSkeleton, EditorPageSkeleton, KnowledgePageSkeleton } from './routes/skeletons';

const LandingPage = lazy(() => import('./routes/LandingPage'));
const ConversationsRoute = lazy(() => import('./routes/ConversationsRoute'));
const EditorPage = lazy(() => import('./routes/EditorPage'));
const KnowledgePage = lazy(() => import('./routes/KnowledgePage'));
const DatabasePage = lazy(() => import('./routes/DatabasePage'));

// Applies the active theme once for every route, and renders the single
// shared WorkspacePickerModal instance (fed entirely from context) so each
// page doesn't need to mount/import its own copy.
function ThemedShell({ children }) {
  const { antTheme, workspacePickerMode, setWorkspacePickerMode, handleWorkspacePickerSelect, projects } = useAppState();
  return (
    <ConfigProvider theme={antTheme}>
      {/* antd's App component provides the context FileTree's useApp() needs
          for its delete-confirmation dialog to render themed, instead of
          falling back to antd's unstyled static Modal/message API. */}
      <AntApp>
        {children}
        <WorkspacePickerModal
          open={Boolean(workspacePickerMode)}
          mode={workspacePickerMode || 'folder'}
          projects={projects}
          onCancel={() => setWorkspacePickerMode(null)}
          onSelect={handleWorkspacePickerSelect}
        />
      </AntApp>
    </ConfigProvider>
  );
}

export default function App() {
  return (
    <AppStateProvider>
      <ThemedShell>
        <Routes>
          <Route path="/" element={<Suspense fallback={<LandingSkeleton />}><LandingPage /></Suspense>} />
          <Route path="/conversations" element={<Suspense fallback={<EditorPageSkeleton />}><ConversationsRoute /></Suspense>} />
          <Route path="/projects/:projectId" element={<Suspense fallback={<EditorPageSkeleton />}><EditorPage /></Suspense>} />
          <Route path="/projects/:projectId/files/*" element={<Suspense fallback={<EditorPageSkeleton />}><EditorPage /></Suspense>} />
          <Route path="/projects/:projectId/knowledge" element={<Suspense fallback={<KnowledgePageSkeleton />}><KnowledgePage /></Suspense>} />
          <Route path="/projects/:projectId/db" element={<Suspense fallback={<KnowledgePageSkeleton />}><DatabasePage /></Suspense>} />
          <Route path="*" element={<Navigate to="/" replace />} />
        </Routes>
      </ThemedShell>
    </AppStateProvider>
  );
}
