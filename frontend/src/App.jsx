import React, { useState, useEffect, useRef, useCallback } from 'react';
import { 
  Typography, Button, Tabs, ConfigProvider, Tooltip,
  Card, Space, Avatar, Breadcrumb, Progress, theme as antdTheme
} from 'antd';
import {
  FolderOpenOutlined,
  FileOutlined,
  PartitionOutlined,
  SaveOutlined,
  SettingOutlined,
  SyncOutlined,
  SearchOutlined,
  AppstoreOutlined,
  UserOutlined,
  BranchesOutlined,
  CloseOutlined,
  PlusOutlined,
  EllipsisOutlined,
  StarFilled,
  MessageOutlined,
  CodeOutlined,
  DatabaseOutlined,
  LoadingOutlined,
  MinusOutlined
} from '@ant-design/icons';
import FileTree from './components/FileTree';
import EditorPanel from './components/EditorPanel';
import ChatPanel from './components/ChatPanel';
import ConversationsPage from './components/ConversationsPage';
import TopNavbar from './components/TopNavbar';
import ProjectKnowledgePage from './components/ProjectKnowledgePage';
import ThemeSettingsModal from './components/ThemeSettingsModal';
import WorkspacePickerModal from './components/WorkspacePickerModal';
import { useEditorContext } from './useEditorContext';
import { getStudioTheme, themeVariables } from './themes';

const { Title, Text } = Typography;

const folderNameFromPath = (path) => path?.split(/[\\/]/).filter(Boolean).pop() || 'Workspace';

// ─── Resizable Drag Handle ───────────────────────────────────────────────
function DragHandle({ onDrag, onStart, direction = 'vertical' }) {
  const isVertical = direction === 'vertical';

  const onMouseDown = useCallback((e) => {
    e.preventDefault();
    if (onStart) onStart();
    const startPos = isVertical ? e.clientX : e.clientY;

    const onMouseMove = (moveEvt) => {
      const currentPos = isVertical ? moveEvt.clientX : moveEvt.clientY;
      onDrag(currentPos, startPos);
    };

    const onMouseUp = () => {
      document.removeEventListener('mousemove', onMouseMove);
      document.removeEventListener('mouseup', onMouseUp);
      document.body.style.cursor = '';
      document.body.style.userSelect = '';
    };

    document.body.style.cursor = isVertical ? 'col-resize' : 'row-resize';
    document.body.style.userSelect = 'none';
    document.addEventListener('mousemove', onMouseMove);
    document.addEventListener('mouseup', onMouseUp);
  }, [onDrag, onStart, isVertical]);

  return (
    <div
      onMouseDown={onMouseDown}
      style={{
        ...(isVertical
          ? { width: 4, cursor: 'col-resize' }
          : { height: 4, cursor: 'row-resize' }),
        backgroundColor: 'var(--studio-border, #d8d1c5)',
        flexShrink: 0,
        transition: 'background-color 0.15s',
        zIndex: 10,
      }}
      onMouseOver={(e) => e.currentTarget.style.backgroundColor = 'var(--studio-accent, #c15f3c)'}
      onMouseOut={(e) => e.currentTarget.style.backgroundColor = 'var(--studio-border, #d8d1c5)'}
    />
  );
}

function StatusBar({ indexStatus, scanMinimized, onScanMinimize, onScanExpand }) {
  const scanning = indexStatus.status === 'scanning' || indexStatus.status === 'running';
  const enriching = indexStatus.enrichmentStatus === 'queued' || indexStatus.enrichmentStatus === 'running';
  const busy = scanning || enriching;
  const completed = (indexStatus.processed || 0) + (indexStatus.skipped || 0);
  const percent = indexStatus.total ? Math.min(100, Math.round((completed / indexStatus.total) * 100)) : 0;
  const activity = scanning
    ? (indexStatus.total > 0 ? `Building code index ${completed}/${indexStatus.total}` : 'Scanning workspace…')
    : `Enriching AI knowledge${indexStatus.enrichmentRemaining > 0 ? ` · ${indexStatus.enrichmentRemaining} remaining` : '…'}`;

  return (
    <div className={`app-status-bar ${busy ? 'is-scanning' : ''}`}>
      <Space size="middle">
        <span className="status-item"><BranchesOutlined /> main</span>
        <span className="status-item"><CloseOutlined style={{ fontSize: 10 }} /> 0</span>
        {busy && (scanMinimized ? (
          <button type="button" className="scan-compact" onClick={onScanExpand} title="Show indexing progress">
            <LoadingOutlined spin /> {scanning ? 'Indexing code' : 'AI knowledge'} in background
          </button>
        ) : (
          <div className="scan-notification" role="status" aria-live="polite">
            <LoadingOutlined spin />
            <span>{activity}</span>
            {scanning && <Progress percent={percent} showInfo={false} size="small" className="scan-progress" />}
            <Button type="text" size="small" icon={<MinusOutlined />} onClick={onScanMinimize}>Run in background</Button>
          </div>
        ))}
        {indexStatus.status === 'completed_with_errors' && (
          <Tooltip title={`${indexStatus.errors || 0} indexing errors`}>
            <span className="status-item status-warning"><DatabaseOutlined /> Indexed with warnings</span>
          </Tooltip>
        )}
      </Space>
      <Space size="middle" className="status-right">
        <span>Ln 1, Col 1</span><span>UTF-8</span><span>Go</span><span>Prettier</span>
      </Space>
    </div>
  );
}

// ─── Main App ────────────────────────────────────────────────────────────
export default function App() {
  const [themeID, setThemeID] = useState(() => window.localStorage.getItem('nativestudio.theme') || 'default');
  const [themeSettingsOpen, setThemeSettingsOpen] = useState(false);
  const [workspacePickerMode, setWorkspacePickerMode] = useState(null);
  const [projects, setProjects] = useState([]);
  const [activeProject, setActiveProject] = useState(null);
  const [activeView, setActiveView] = useState('editor');
  const [requestedSessionId, setRequestedSessionId] = useState('');
  const [aiPreview, setAiPreview] = useState(null);
  const [layout, setLayout] = useState({ folders: true, code: true, ai: true });
  const [indexStatus, setIndexStatus] = useState({ status: 'idle', processed: 0, total: 0, skipped: 0, errors: 0 });
  const [scanNotificationMinimized, setScanNotificationMinimized] = useState(false);

  const editorCtx = useEditorContext();

  const [openFiles, setOpenFiles] = useState([]);
  const [activeTab, setActiveTab] = useState('');
  const [refreshTrigger, setRefreshTrigger] = useState(0);

  const [sidebarWidth, setSidebarWidth] = useState(260);
  const [chatWidth, setChatWidth] = useState(360);
  const sidebarStartRef = useRef(260);
  const chatStartRef = useRef(360);
  const activeTheme = getStudioTheme(themeID);
  const studioStyle = themeVariables(activeTheme);
  const studioClassName = `theme-shell studio-layout-${activeTheme.layout} ${activeTheme.dark ? 'is-dark' : 'is-light'}`;
  const antTheme = {
    algorithm: activeTheme.dark ? antdTheme.darkAlgorithm : antdTheme.defaultAlgorithm,
    token: {
      colorPrimary: activeTheme.accent,
      colorInfo: activeTheme.accent,
      colorBgBase: activeTheme.bg,
      colorBgContainer: activeTheme.surface,
      colorBgElevated: activeTheme.surface,
      colorBorder: activeTheme.border,
      colorTextBase: activeTheme.text,
      fontFamily: activeTheme.font,
      borderRadius: activeTheme.radius,
    },
    components: {
      Tree: { nodeSelectedBg: activeTheme.accentSoft, nodeHoverBg: activeTheme.panel, colorBgContainer: 'transparent' },
      Tabs: { colorBgContainer: activeTheme.surface, colorBorderSecondary: activeTheme.border },
    },
  };

  const selectTheme = id => {
    setThemeID(id);
    window.localStorage.setItem('nativestudio.theme', id);
  };

  useEffect(() => {
    const variables = themeVariables(getStudioTheme(themeID));
    Object.entries(variables).forEach(([name, value]) => document.documentElement.style.setProperty(name, value));
  }, [themeID]);

  useEffect(() => {
    let cancelled = false;
    fetch('/api/projects')
      .then(res => res.json())
      .then(data => { if (!cancelled) setProjects(data.projects || []); })
      .catch(error => console.error(error));
    return () => { cancelled = true; };
  }, []);

  useEffect(() => {
    if (!activeProject?.id) return undefined;

    let cancelled = false;

    const refreshStatus = async () => {
      try {
        const response = await fetch(`/api/projects/${activeProject.id}/index/status`);
        if (!response.ok) return;
        const status = await response.json();
        if (!cancelled) setIndexStatus(current => {
          if ((current.status === 'scanning' || current.status === 'running') && status.status === 'not_indexed') return current;
          return {
            ...current,
            ...status,
            enrichmentStatus: status.enrichment_status || current.enrichmentStatus,
            enrichmentRemaining: status.enrichment_remaining ?? current.enrichmentRemaining,
          };
        });
      } catch {
        // The event stream will continue reconnecting if the backend is restarting.
      }
    };

    const source = new EventSource(`/api/projects/${activeProject.id}/index/events`);
    source.onmessage = event => {
      if (cancelled) return;
      try {
        const update = JSON.parse(event.data);
        const data = update.data || {};
        if (update.type === 'knowledge_reset') {
          setIndexStatus({ status: 'scanning', processed: 0, total: 0, skipped: 0, errors: 0 });
          setScanNotificationMinimized(false);
        } else if (update.type === 'index_started' || update.type === 'index_progress') {
          setIndexStatus(current => ({ ...current, ...data, status: 'running' }));
          if (update.type === 'index_started' && (data.total || 0) > 1) setScanNotificationMinimized(false);
        } else if (update.type === 'index_completed') {
          setIndexStatus(current => ({ ...current, ...data, status: data.errors ? 'completed_with_errors' : 'completed' }));
        } else if (update.type === 'enrichment_queued') {
          setIndexStatus(current => ({ ...current, enrichmentStatus: 'queued', enrichmentRemaining: data.remaining || 0 }));
        } else if (update.type === 'enrichment_started' || update.type === 'enrichment_progress' || update.type === 'enrichment_aggregating') {
          setIndexStatus(current => ({ ...current, enrichmentStatus: 'running', enrichmentRemaining: data.remaining || 0, enrichmentPath: data.path || '' }));
        } else if (update.type === 'enrichment_completed') {
          setIndexStatus(current => ({ ...current, enrichmentStatus: 'completed', enrichmentRemaining: 0, enrichmentPath: '' }));
        } else if (update.type === 'index_cancelled') {
          setIndexStatus(current => ({ ...current, ...data, status: 'cancelled' }));
        } else if (update.type === 'index_error' && !data.path && !data.stage) {
          setIndexStatus(current => ({ ...current, ...data, status: 'failed' }));
        }
      } catch {
        // Ignore malformed events and let the status poll restore authoritative state.
      }
    };

    const poll = window.setInterval(refreshStatus, 1000);
    return () => {
      cancelled = true;
      window.clearInterval(poll);
      source.close();
    };
  }, [activeProject?.id]);

  const handleCreateProject = () => setWorkspacePickerMode('folder');

  const createProjectFromPath = async path => {
    try {
      const name = folderNameFromPath(path);

      const res = await fetch('/api/projects', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name, path, description: '' })
      });
      const data = await res.json();
      if (res.ok) {
        setProjects([data.project, ...projects]);
        selectProject(data.project);
      }
    } catch (e) { console.error(e); }
  };

  const handleOpenFile = () => setWorkspacePickerMode('file');

  const openFileFromPath = async filePath => {
    try {
      const parentPath = filePath.replace(/[\\/][^\\/]+$/, '') || '/';
      const normalizedParent = parentPath.replace(/[\\/]+$/, '');
      let project = projects.find(item => item.path.replace(/[\\/]+$/, '') === normalizedParent);
      if (!project) {
        const createRes = await fetch('/api/projects', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ name: folderNameFromPath(parentPath), path: parentPath, description: '' }),
        });
        const createData = await createRes.json();
        if (!createRes.ok) throw new Error('Could not open the file folder');
        project = createData.project;
        setProjects(items => [project, ...items]);
      }

      await selectProject(project);
      const name = folderNameFromPath(filePath);
      const workspaceFilePath = `/${name}`;
      setOpenFiles([{ path: workspaceFilePath, name }]);
      setActiveTab(workspaceFilePath);
      editorCtx.setActiveFile(workspaceFilePath);
      editorCtx.addOpenFile(workspaceFilePath);
    } catch (error) {
      console.error(error);
    }
  };

  const handleWorkspacePickerSelect = async path => {
    const mode = workspacePickerMode;
    setWorkspacePickerMode(null);
    if (mode === 'folder') await createProjectFromPath(path);
    if (mode === 'file') await openFileFromPath(path);
  };

  const workspacePicker = (
    <WorkspacePickerModal
      open={Boolean(workspacePickerMode)}
      mode={workspacePickerMode || 'folder'}
      projects={projects}
      onCancel={() => setWorkspacePickerMode(null)}
      onSelect={handleWorkspacePickerSelect}
    />
  );

  async function selectProject(proj) {
    try {
      const res = await fetch('/api/workspace', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ project_id: proj.id, path: proj.path })
      });
      if (res.ok) {
        setIndexStatus({ status: 'scanning', processed: 0, total: 0, skipped: 0, errors: 0 });
        setScanNotificationMinimized(false);
        setActiveProject(proj);
        setActiveView('editor');
        setRefreshTrigger(prev => prev + 1);
        setOpenFiles([]);
        setActiveTab('');
      }
    } catch (e) { console.error(e); }
  }

  const handleFileClick = (path) => {
    const name = path.substring(path.lastIndexOf('/') + 1);
    if (!openFiles.find(f => f.path === path)) {
      setOpenFiles([...openFiles, { path, name }]);
    }
    setActiveTab(path);
    editorCtx.setActiveFile(path);
    editorCtx.addOpenFile(path);
  };

  const handleTabChange = (key) => {
    setActiveTab(key);
    editorCtx.setActiveFile(key);
  };

  const handleTabEdit = (targetKey, action) => {
    if (action === 'remove') {
      const newFiles = openFiles.filter(f => f.path !== targetKey);
      setOpenFiles(newFiles);
      editorCtx.removeOpenFile(targetKey);
      if (activeTab === targetKey) {
        const nextTab = newFiles.length > 0 ? newFiles[newFiles.length - 1].path : '';
        setActiveTab(nextTab);
        editorCtx.setActiveFile(nextTab);
      }
    }
  };

  const handleSidebarDrag = useCallback((currentX, startX) => {
    const delta = currentX - startX;
    const newW = Math.max(180, Math.min(500, sidebarStartRef.current + delta));
    setSidebarWidth(newW);
  }, []);

  const handleChatDrag = useCallback((currentX, startX) => {
    const delta = currentX - startX;
    const newW = Math.max(280, Math.min(700, chatStartRef.current - delta));
    setChatWidth(newW);
  }, []);

  const toggleLayout = (pane) => {
    setLayout(current => ({ ...current, [pane]: !current[pane] }));
  };

  const handleOpenConversation = async (conversation) => {
    const project = projects.find(item => item.id === conversation.project_id);
    if (!project) return;
    setRequestedSessionId(conversation.id);
    await selectProject(project);
    setActiveView('editor');
  };

  // ─── Project Selector Landing ────────────────────────────────────────
  if (!activeProject && activeView !== 'conversations') {
    return (
      <ConfigProvider theme={antTheme}>
        <div className={studioClassName} style={{
          ...studioStyle,
          display: 'flex', flexDirection: 'column', height: '100vh', width: '100vw',
          backgroundColor: 'var(--studio-bg, #f7f4ed)',
          alignItems: 'center', justifyContent: 'center',
        }}>
          <Card style={{
            width: 480, maxWidth: '90vw', 
            backgroundColor: 'var(--studio-surface, #fffdf8)',
            border: '1px solid var(--studio-border, #d8d1c5)',
            borderRadius: 16,
            boxShadow: '0 18px 60px rgba(67,55,45,.10)',
          }} styles={{ body: { paddingTop: 40, paddingLeft: 32, paddingRight: 32 } }}>
            <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'center', gap: 12, marginBottom: 4 }}>
              <CodeOutlined style={{ fontSize: 32, color: 'var(--studio-accent, #c15f3c)' }} />
              <Title level={3} style={{ margin: 0, fontWeight: 700, color: 'var(--studio-text, #2f2a26)', letterSpacing: '-0.02em' }}>
                nativestudio
              </Title>
            </div>
            <div style={{ textAlign: 'center', marginBottom: 32, color: 'var(--studio-muted, #746b63)' }}>
              Select a project or open a new folder to begin.
            </div>

            <div style={{ backgroundColor: 'var(--studio-panel, #f5f1e9)', border: '1px solid var(--studio-border, #d8d1c5)', borderRadius: 12, maxHeight: 260, overflow: 'auto' }}>
              {projects.length === 0 ? (
                <div style={{ padding: 24, textAlign: 'center', color: 'var(--studio-muted, #746b63)' }}>
                  <div>No projects yet.</div>
                  <div>Open a folder to get started.</div>
                </div>
              ) : (
                projects.map(p => (
                  <div
                    key={p.id || p.path}
                    onClick={() => selectProject(p)}
                    style={{ cursor: 'pointer', padding: '12px 16px', borderBottom: '1px solid var(--studio-border, #d8d1c5)', display: 'flex', alignItems: 'flex-start', gap: 12 }}
                    onMouseOver={(e) => e.currentTarget.style.backgroundColor = 'var(--studio-panel, #eee9df)'}
                    onMouseOut={(e) => e.currentTarget.style.backgroundColor = 'transparent'}
                  >
                    <PartitionOutlined style={{ color: 'var(--studio-accent, #c15f3c)', fontSize: 20, marginTop: 2 }} />
                    <div style={{ display: 'flex', flexDirection: 'column' }}>
                      <Text strong style={{ fontSize: '15px', color: 'var(--studio-text, #2f2a26)', lineHeight: 1.2 }}>{folderNameFromPath(p.path)}</Text>
                      <Text type="secondary" style={{ fontSize: '12px', color: 'var(--studio-muted, #746b63)', fontFamily: 'monospace', marginTop: 2 }}>{p.path}</Text>
                    </div>
                  </div>
                ))
              )}
            </div>
            
            <div style={{ display: 'flex', justifyContent: 'center', marginTop: 24, marginBottom: 16 }}>
              <Button type="primary" size="large" icon={<FolderOpenOutlined />} onClick={handleCreateProject} style={{ padding: '0 40px', height: 44, borderRadius: 8 }}>
                Open Folder
              </Button>
            </div>
            {projects.length > 0 && (
              <Button type="link" block icon={<MessageOutlined />} onClick={() => setActiveView('conversations')}>
                View conversations & memory
              </Button>
            )}
          </Card>
          {workspacePicker}
        </div>
      </ConfigProvider>
    );
  }

  const folderName = activeProject ? folderNameFromPath(activeProject.path) : '';

  if (activeView === 'conversations') {
    return (
      <ConfigProvider theme={antTheme}>
        <div className={studioClassName} style={{ ...studioStyle, display: 'flex', flexDirection: 'column', width: '100vw', height: '100vh', overflow: 'hidden', background: 'var(--studio-bg, #f7f4ed)' }}>
          <TopNavbar folderName="" onNavigate={setActiveView} onOpenFile={handleOpenFile} onOpenFolder={handleCreateProject} />
          <ConversationsPage onOpenConversation={handleOpenConversation} />
          {workspacePicker}
        </div>
      </ConfigProvider>
    );
  }

  if (activeView === 'knowledge' && activeProject) {
    return (
      <ConfigProvider theme={antTheme}>
        <div className={studioClassName} style={{ ...studioStyle, display: 'flex', flexDirection: 'column', width: '100vw', height: '100vh', overflow: 'hidden', background: 'var(--studio-bg, #f7f4ed)' }}>
          <TopNavbar folderName={folderNameFromPath(activeProject.path)} onNavigate={setActiveView} onOpenFile={handleOpenFile} onOpenFolder={handleCreateProject} />
          <ProjectKnowledgePage project={activeProject} onBack={() => setActiveView('editor')} onRelearnStart={() => {
            setIndexStatus({ status: 'scanning', processed: 0, total: 0, skipped: 0, errors: 0 });
            setScanNotificationMinimized(false);
          }} onOpenFile={(path, source) => {
            handleFileClick(`/${path.replace(/^\/+/, '')}`);
            setActiveView('editor');
            if (source?.start_line) requestAnimationFrame(() => requestAnimationFrame(() => {
              window.editor?.revealLineInCenter(source.start_line);
              window.editor?.setSelection({ startLineNumber: source.start_line, startColumn: 1, endLineNumber: source.end_line || source.start_line, endColumn: 1 });
            }));
          }} />
          <StatusBar indexStatus={indexStatus} scanMinimized={scanNotificationMinimized} onScanMinimize={() => setScanNotificationMinimized(true)} onScanExpand={() => setScanNotificationMinimized(false)} />
          {workspacePicker}
        </div>
      </ConfigProvider>
    );
  }

  const breadcrumbItems = activeTab
    ? [folderName, ...activeTab.split(/[\\/]/).filter(Boolean)].map((title, index, items) => ({
        title: index === items.length - 1
          ? <Text style={{ color: 'var(--studio-text, #2f2a26)', fontSize: 12 }}>{title}</Text>
          : title,
      }))
    : [];

  // ─── Main IDE Layout ─────────────────────────────────────────────────
  return (
    <ConfigProvider theme={antTheme}>
      <div className={studioClassName} style={{ ...studioStyle, display: 'flex', flexDirection: 'column', height: '100vh', width: '100vw', overflow: 'hidden', backgroundColor: 'var(--studio-bg, #f7f4ed)', color: 'var(--studio-text, #2f2a26)' }}>

        <TopNavbar
          folderName={folderName}
          onNavigate={setActiveView}
          onOpenFile={handleOpenFile}
          onOpenFolder={handleCreateProject}
          layout={layout}
          onToggleLayout={toggleLayout}
        />

        <div className="studio-main" style={{ display: 'flex', flexGrow: 1, overflow: 'hidden', width: '100%' }}>
          
          {/* ─── Activity Bar (Far Left) ─────────────────────────────────── */}
          <div className="activity-bar" style={{
            width: 48, minWidth: 48, display: 'flex', flexDirection: 'column', alignItems: 'center', 
            backgroundColor: 'var(--studio-surface, #fffdf8)', borderRight: '1px solid var(--studio-border, #d8d1c5)', padding: '16px 0', gap: 24
          }}>
            {/* Top Icons */}
            <div style={{ flexGrow: 1, display: 'flex', flexDirection: 'column', alignItems: 'center', gap: 24, width: '100%' }}>
              <Tooltip placement="right" title="Explorer">
                <div
                  onClick={() => toggleLayout('folders')}
                  style={{ borderLeft: layout.folders ? '2px solid var(--studio-accent, #c15f3c)' : '2px solid transparent', width: '100%', display: 'flex', justifyContent: 'center', cursor: 'pointer' }}
                >
                  <FileOutlined style={{ fontSize: 24, color: layout.folders ? 'var(--studio-text, #2f2a26)' : 'var(--studio-subtle, #91877e)' }} />
                </div>
              </Tooltip>
              <Tooltip placement="right" title="Search">
                <SearchOutlined style={{ fontSize: 24, color: 'var(--studio-subtle, #91877e)', cursor: 'pointer' }} />
              </Tooltip>
              <Tooltip placement="right" title="Source Control">
                <BranchesOutlined style={{ fontSize: 24, color: 'var(--studio-subtle, #91877e)', cursor: 'pointer' }} />
              </Tooltip>
              <Tooltip placement="right" title="Extensions">
                <AppstoreOutlined style={{ fontSize: 24, color: 'var(--studio-subtle, #91877e)', cursor: 'pointer' }} />
              </Tooltip>
              <Tooltip placement="right" title="Project Knowledge">
                <DatabaseOutlined onClick={() => setActiveView('knowledge')} style={{ fontSize: 24, color: 'var(--studio-subtle, #91877e)', cursor: 'pointer' }} />
              </Tooltip>
            </div>

            {/* Bottom Actions */}
            <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', gap: 24, width: '100%', paddingBottom: 8 }}>
              <Tooltip placement="right" title="Settings">
                <SettingOutlined onClick={() => setThemeSettingsOpen(true)} style={{ fontSize: 24, color: 'var(--studio-subtle, #91877e)', cursor: 'pointer' }} />
              </Tooltip>
              <Avatar icon={<UserOutlined />} size={24} style={{ backgroundColor: 'var(--studio-border, #d8d1c5)', color: 'var(--studio-text, #2f2a26)', cursor: 'pointer' }} />
            </div>
          </div>

          {/* ─── Sidebar (File Explorer) ────────────────────────────────── */}
          {layout.folders && <>
            <div className="explorer-panel" style={{
              width: sidebarWidth, minWidth: sidebarWidth, maxWidth: sidebarWidth,
              flexShrink: 0, display: 'flex', flexDirection: 'column',
              backgroundColor: 'var(--studio-bg, #f7f4ed)', borderRight: '1px solid var(--studio-border, #d8d1c5)', overflow: 'hidden',
            }}>
            {/* Explorer Header */}
            <div style={{ 
              height: 35, display: 'flex', alignItems: 'center', padding: '0 16px',
              justifyContent: 'space-between', marginTop: 8
            }}>
              <Text style={{ fontSize: '11px', color: 'var(--studio-muted, #746b63)', letterSpacing: 1, fontWeight: 600 }}>
                EXPLORER
              </Text>
              <Space size={2}>
                <Button type="text" size="small" icon={<PlusOutlined style={{ color: 'var(--studio-muted, #746b63)', fontSize: 12 }} />} onClick={handleCreateProject} />
                <Button type="text" size="small" icon={<EllipsisOutlined style={{ color: 'var(--studio-muted, #746b63)', fontSize: 12 }} />} />
              </Space>
            </div>

            {/* Project Name Header */}
            <div style={{ 
              display: 'flex', alignItems: 'center', padding: '4px 16px',
              fontSize: '11px', fontWeight: 600, color: 'var(--studio-text, #2f2a26)', textTransform: 'uppercase', cursor: 'pointer'
            }}>
              <FolderOpenOutlined style={{ marginRight: 6 }} /> {folderName}
            </div>

            {/* Tree Area */}
            <div style={{ flex: 1, overflow: 'hidden' }}>
              <FileTree onFileClick={handleFileClick} refreshTrigger={refreshTrigger} />
            </div>
            </div>

            <DragHandle direction="vertical" onDrag={handleSidebarDrag} onStart={() => { sidebarStartRef.current = sidebarWidth; }} />
          </>}

          {/* ─── Main Editor Area ───────────────────────────────────────── */}
          {layout.code && <div className="editor-workbench" style={{
            flex: 1, display: 'flex', flexDirection: 'column', 
            minWidth: 0, maxWidth: '100%', overflow: 'hidden',
            backgroundColor: 'var(--studio-bg, #f7f4ed)'
          }}>
            
            {/* Tab bar */}
            <div style={{ display: 'flex', backgroundColor: 'var(--studio-surface, #fffdf8)', overflowX: 'auto' }}>
              {openFiles.length > 0 && (
                <Tabs
                  className="borderless-file-tabs"
                  type="editable-card"
                  hideAdd
                  onChange={handleTabChange}
                  activeKey={activeTab}
                  onEdit={handleTabEdit}
                  style={{ width: '100%', margin: 0 }}
                  size="small"
                  items={openFiles.map(f => ({
                    key: f.path,
                    label: (
                      <span style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
                        <FileOutlined style={{ color: 'var(--studio-accent, #c15f3c)' }} />
                        {f.name}
                      </span>
                    ),
                    closable: true
                  }))}
                />
              )}
            </div>

            {/* Breadcrumbs */}
            {activeTab && (
              <div style={{ minHeight: 29, padding: '4px 16px', overflowX: 'auto', backgroundColor: 'var(--studio-bg, #f7f4ed)', borderBottom: '1px solid var(--studio-border, #d8d1c5)' }}>
                <Breadcrumb separator="›" items={breadcrumbItems} style={{ whiteSpace: 'nowrap', fontSize: 12 }} />
              </div>
            )}

            {/* Editor content */}
            <div style={{ flex: 1, overflow: 'hidden', position: 'relative' }}>
              {activeTab ? (
                <EditorPanel
                  filepath={activeTab}
                  darkMode={activeTheme.dark}
                  aiPreview={aiPreview}
                  onCursorChange={editorCtx.updateCursor}
                  onSelectionChange={editorCtx.updateSelection}
                  onSymbolChange={editorCtx.updateSelectedSymbol}
                  onEdit={editorCtx.recordEdit}
                />
              ) : (
                <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', justifyContent: 'center', height: '100%', gap: 16 }}>
                  <CodeOutlined style={{ fontSize: 56, color: 'var(--studio-border, #d8d1c5)' }} />
                  <Text style={{ color: 'var(--studio-muted, #746b63)' }}>Select a file from the tree to start editing</Text>
                  <Text style={{ fontSize: '12px', color: 'var(--studio-subtle, #91877e)' }}>⌘S to save · Ctrl+Enter to send to AI</Text>
                </div>
              )}
            </div>
          </div>}

          {layout.code && layout.ai && (
            <DragHandle direction="vertical" onDrag={handleChatDrag} onStart={() => { chatStartRef.current = chatWidth; }} />
          )}

          {/* ─── Chat Panel ─────────────────────────────────────────────── */}
          {layout.ai && <div className={`chat-shell ${!layout.code ? 'chat-shell-expanded' : ''}`} style={{ 
            width: layout.code ? chatWidth : 'auto', minWidth: layout.code ? chatWidth : 0, maxWidth: layout.code ? chatWidth : 'none',
            flexShrink: 0, display: 'flex', flexDirection: 'column',
            backgroundColor: 'var(--studio-bg, #f7f4ed)', borderLeft: '1px solid var(--studio-border, #d8d1c5)', overflow: 'hidden',
          }}>
            <ChatPanel
              activeFilePath={activeTab}
              activeProject={activeProject}
              editorContext={editorCtx.context}
              requestedSessionId={requestedSessionId}
              onOpenFile={handleFileClick}
              onPreviewChange={setAiPreview}
              onPreviewClear={() => setAiPreview(null)}
            />
          </div>}

        </div>

        {/* ─── Status Bar (Bottom) ────────────────────────────────────── */}
        <StatusBar indexStatus={indexStatus} scanMinimized={scanNotificationMinimized} onScanMinimize={() => setScanNotificationMinimized(true)} onScanExpand={() => setScanNotificationMinimized(false)} />

        <ThemeSettingsModal open={themeSettingsOpen} selectedTheme={themeID} onSelect={selectTheme} onClose={() => setThemeSettingsOpen(false)} />
        {workspacePicker}

      </div>
    </ConfigProvider>
  );
}
