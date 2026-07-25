import React, { useState, useEffect, useRef, useCallback } from 'react';
import { useNavigate } from 'react-router-dom';
import { theme as antdTheme } from 'antd';
import { useEditorContext } from '../useEditorContext';
import { getStudioTheme, themeVariables } from '../themes';
import { fileURL } from './fileURL';
import { folderNameFromPath } from './folderNameFromPath';
import { AppStateContext } from './context';
import { loadFontSettings, saveFontSettings, DEFAULT_FONT_SETTINGS } from './fontSettings';

// --- Editor Settings ---
const DEFAULT_EDITOR_SETTINGS = {
  minimap: true,
  wordWrap: 'off',
  smoothScrolling: true,
  cursorBlinking: 'smooth',
  renderLineHighlight: 'all',
  renderWhitespace: 'none',
  autoClosingBrackets: 'always', // 'always', 'languageDefined', 'beforeWhitespace', 'never'
  formatOnType: true,
  codeLens: true,
  bracketPairColorization: true,
  linkedEditing: true,
  matchBrackets: 'always', // 'always', 'never', 'near'
};

export function AppStateProvider({ children }) {
  const navigate = useNavigate();

  const [themeID, setThemeID] = useState('default');
  const [themeSettingsOpen, setThemeSettingsOpen] = useState(false);
  const [fontSettingsOpen, setFontSettingsOpen] = useState(false);
  const [fontSettings, setFontSettings] = useState(DEFAULT_FONT_SETTINGS);
  const [editorSettings, setEditorSettings] = useState(DEFAULT_EDITOR_SETTINGS);
  const [settingsLoaded, setSettingsLoaded] = useState(false);

  const [workspacePickerMode, setWorkspacePickerMode] = useState(null);
  const [projects, setProjects] = useState([]);
  const [projectsLoaded, setProjectsLoaded] = useState(false);
  const [activeProject, setActiveProject] = useState(null);
  const [requestedSessionId, setRequestedSessionId] = useState('');
  const [aiPreview, setAiPreview] = useState(null);
  const [layout, setLayout] = useState({ folders: true, code: true, ai: true });
  const [indexStatus, setIndexStatus] = useState({
    status: 'idle', processed: 0, total: 0, skipped: 0, errors: 0,
    enrichmentStatus: 'idle', enrichmentRemaining: 0, enrichmentPath: '',
    enrichmentEmbedded: 0, enrichmentSummaries: 0, enrichmentDurationMs: 0,
  });
  const [scanNotificationMinimized, setScanNotificationMinimized] = useState(false);
  // Per-file timing accumulator — key: path, value: start timestamp ms
  const enrichStartTimes = useRef({});

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

  useEffect(() => {
    let cancelled = false;
    fetch('/api/settings')
      .then(r => r.json())
      .then(data => {
        if (cancelled) return;
        if (data.themeID) setThemeID(data.themeID);
        if (data.fontSettings) setFontSettings({ ...DEFAULT_FONT_SETTINGS, ...data.fontSettings });
        if (data.editorSettings) setEditorSettings({ ...DEFAULT_EDITOR_SETTINGS, ...data.editorSettings });
        setSettingsLoaded(true);
      })
      .catch(err => {
        console.error('Failed to load settings:', err);
        if (!cancelled) setSettingsLoaded(true);
      });
    return () => { cancelled = true; };
  }, []);

  const saveSettingsToAPI = (theme, font, editor) => {
    fetch('/api/settings', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ themeID: theme, fontSettings: font, editorSettings: editor })
    }).catch(console.error);
  };

  const selectTheme = id => {
    setThemeID(id);
    saveSettingsToAPI(id, fontSettings, editorSettings);
  };

  const updateFontSettings = patch => {
    setFontSettings(prev => {
      const next = { ...prev, ...patch };
      saveSettingsToAPI(themeID, next, editorSettings);
      return next;
    });
  };

  const updateEditorSettings = (updates) => {
    setEditorSettings(prev => {
      const next = { ...prev, ...updates };
      saveSettingsToAPI(themeID, fontSettings, next);
      return next;
    });
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
      .catch(error => console.error(error))
      .finally(() => { if (!cancelled) setProjectsLoaded(true); });
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
          setIndexStatus({ status: 'scanning', processed: 0, total: 0, skipped: 0, errors: 0, enrichmentStatus: 'idle', enrichmentRemaining: 0, enrichmentPath: '', enrichmentEmbedded: 0, enrichmentSummaries: 0, enrichmentDurationMs: 0 });
          setScanNotificationMinimized(false);
          enrichStartTimes.current = {};
        } else if (update.type === 'index_started' || update.type === 'index_progress') {
          setIndexStatus(current => ({ ...current, ...data, status: 'running' }));
          if (update.type === 'index_started' && (data.total || 0) > 1) setScanNotificationMinimized(false);
        } else if (update.type === 'index_completed') {
          setIndexStatus(current => ({ ...current, ...data, status: data.errors ? 'completed_with_errors' : 'completed' }));
        } else if (update.type === 'enrichment_queued') {
          setIndexStatus(current => ({ ...current, enrichmentStatus: 'queued', enrichmentRemaining: data.remaining || 0 }));
        } else if (update.type === 'enrichment_started') {
          if (data.path) enrichStartTimes.current[data.path] = Date.now();
          setIndexStatus(current => ({ ...current, enrichmentStatus: 'running', enrichmentRemaining: data.remaining || 0, enrichmentPath: data.path || '' }));
        } else if (update.type === 'enrichment_progress') {
          // Accumulate duration from per-file timing
          const durationMs = data.duration_ms || 0;
          if (data.path) delete enrichStartTimes.current[data.path];
          setIndexStatus(current => ({ ...current, enrichmentStatus: 'running', enrichmentRemaining: data.remaining || 0, enrichmentPath: data.path || '', enrichmentDurationMs: (current.enrichmentDurationMs || 0) + durationMs }));
        } else if (update.type === 'enrichment_aggregating') {
          setIndexStatus(current => ({ ...current, enrichmentStatus: 'aggregating', enrichmentRemaining: 0, enrichmentPath: '' }));
        } else if (update.type === 'enrichment_completed') {
          setIndexStatus(current => ({ ...current, enrichmentStatus: 'done', enrichmentRemaining: 0, enrichmentPath: '' }));
          enrichStartTimes.current = {};
        } else if (update.type === 'embedding_created') {
          setIndexStatus(current => ({ ...current, enrichmentEmbedded: (current.enrichmentEmbedded || 0) + (data.chunks || 1) }));
        } else if (update.type === 'summary_created') {
          setIndexStatus(current => ({ ...current, enrichmentSummaries: (current.enrichmentSummaries || 0) + 1 }));
        } else if (update.type === 'index_cancelled') {
          setIndexStatus(current => ({ ...current, ...data, status: 'cancelled' }));
        } else if (update.type === 'enrichment_cancelled') {
          setIndexStatus(current => ({ ...current, enrichmentStatus: 'cancelled', enrichmentRemaining: 0, enrichmentPath: '' }));
          enrichStartTimes.current = {};
        } else if (update.type === 'enrichment_pending_confirmation') {
          setIndexStatus(current => ({ ...current, enrichmentStatus: 'pending_confirmation', enrichmentRemaining: data.remaining || 0 }));
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
        setRefreshTrigger(prev => prev + 1);
        setOpenFiles([]);
        setActiveTab('');
      }
    } catch (e) { console.error(e); }
  }

  const switchToProject = useCallback(async (proj) => {
    await selectProject(proj);
    navigate(`/projects/${proj.id}`);
  }, [navigate]);

  // Stops both the code-index scan and the enrichment loop (they share one
  // cancellation context server-side) — e.g. a large/slow re-index the user
  // wants to abort, or enrichment they don't want to wait through right now.
  const stopIndexing = useCallback(async () => {
    if (!activeProject) return;
    try {
      await fetch(`/api/projects/${activeProject.id}/index/stop`, { method: 'POST' });
    } catch (e) { console.error(e); }
  }, [activeProject]);

  // Enrichment calls the AI model on every file — scanning starts on its own,
  // but enrichment waits here for the user to explicitly say go, rather than
  // silently burning tokens/CPU the moment a project is opened.
  const approveEnrichment = useCallback(async () => {
    if (!activeProject) return;
    try {
      await fetch(`/api/projects/${activeProject.id}/knowledge/enrichment/approve`, { method: 'POST' });
    } catch (e) { console.error(e); }
  }, [activeProject]);

  const handleCreateProject = () => setWorkspacePickerMode('folder');
  const handleOpenFile = () => setWorkspacePickerMode('file');

  const createProjectFromPath = async path => {
    const normalized = path.replace(/[\\/]+$/, '');
    const existing = projects.find(p => p.path.replace(/[\\/]+$/, '') === normalized);
    if (existing) {
      await switchToProject(existing);
      return;
    }
    try {
      const name = folderNameFromPath(path);
      const res = await fetch('/api/projects', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name, path, description: '' })
      });
      const data = await res.json();
      if (res.ok) {
        setProjects(prev => [data.project, ...prev]);
        await switchToProject(data.project);
      } else {
        console.error('Could not open folder:', data);
      }
    } catch (e) { console.error(e); }
  };

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
      navigate(fileURL(project.id, workspaceFilePath));
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

  const openFileLocal = useCallback((path, cursor) => {
    setOpenFiles(prev => {
      const name = path.substring(path.lastIndexOf('/') + 1);
      return prev.find(f => f.path === path) ? prev : [...prev, { path, name }];
    });
    setActiveTab(path);
    editorCtx.setActiveFile(path);
    editorCtx.addOpenFile(path);
    if (cursor && (cursor.line || cursor.column)) {
      editorCtx.updateCursor(cursor.line || 1, cursor.column || 1);
    }
  }, []);

  const openFile = useCallback((path) => {
    openFileLocal(path);
    if (activeProject) navigate(fileURL(activeProject.id, path));
  }, [openFileLocal, activeProject, navigate]);

  const openFileFromRoute = useCallback((path, cursor) => {
    openFileLocal(path, cursor);
  }, [openFileLocal]);

  const handleTabChange = (key) => {
    setActiveTab(key);
    editorCtx.setActiveFile(key);
    if (activeProject) navigate(fileURL(activeProject.id, key));
  };

  const handleTabEdit = (targetKey, action) => {
    if (action !== 'remove') return;
    const newFiles = openFiles.filter(f => f.path !== targetKey);
    setOpenFiles(newFiles);
    editorCtx.removeOpenFile(targetKey);
    if (activeTab === targetKey) {
      const nextTab = newFiles.length > 0 ? newFiles[newFiles.length - 1].path : '';
      setActiveTab(nextTab);
      editorCtx.setActiveFile(nextTab);
      if (activeProject) navigate(nextTab ? fileURL(activeProject.id, nextTab) : `/projects/${activeProject.id}`);
    }
  };

  const handleFileRenamed = (oldPath, newPath) => {
    const remap = path => (path === oldPath ? newPath : (path.startsWith(oldPath + '/') ? newPath + path.slice(oldPath.length) : path));
    setOpenFiles(files => files.map(f => {
      const remapped = remap(f.path);
      return remapped === f.path ? f : { path: remapped, name: remapped.substring(remapped.lastIndexOf('/') + 1) };
    }));
    const remappedActive = remap(activeTab);
    if (remappedActive !== activeTab) {
      setActiveTab(remappedActive);
      editorCtx.setActiveFile(remappedActive);
      if (activeProject) navigate(fileURL(activeProject.id, remappedActive), { replace: true });
    }
  };

  const handleFileDeleted = (path) => {
    const affected = f => f.path === path || f.path.startsWith(path + '/');
    const remaining = openFiles.filter(f => !affected(f));
    if (remaining.length === openFiles.length) return;
    setOpenFiles(remaining);
    if (affected({ path: activeTab })) {
      const nextTab = remaining.length > 0 ? remaining[remaining.length - 1].path : '';
      setActiveTab(nextTab);
      editorCtx.setActiveFile(nextTab);
      if (activeProject) navigate(nextTab ? fileURL(activeProject.id, nextTab) : `/projects/${activeProject.id}`, { replace: true });
    }
  };

  const cursorSyncTimer = useRef(null);
  const updateCursorAndSync = (line, column) => {
    editorCtx.updateCursor(line, column);
    if (cursorSyncTimer.current) window.clearTimeout(cursorSyncTimer.current);
    cursorSyncTimer.current = window.setTimeout(() => {
      const next = new URLSearchParams(window.location.search);
      next.set('line', String(line));
      next.set('col', String(column));
      const sel = editorCtx.context.selection;
      if (sel) {
        next.set('sel_start', String(sel.start_line));
        next.set('sel_end', String(sel.end_line));
      } else {
        next.delete('sel_start');
        next.delete('sel_end');
      }
      navigate(`${window.location.pathname}?${next.toString()}`, { replace: true });
    }, 400);
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
    await switchToProject(project);
  };

  const handleNavigate = (key) => {
    if (key === 'conversations') navigate('/conversations');
    else if (key === 'knowledge' && activeProject) navigate(`/projects/${activeProject.id}/knowledge`);
    else if (key === 'db' && activeProject) navigate(`/projects/${activeProject.id}/db`);
    else if (key === 'settings') {
      openFile('/__settings__');
    }
  };

  const indexProcesses = (() => {
    const procs = [];
    const s = indexStatus;

    const scanActive = s.status === 'scanning' || s.status === 'running';
    const scanDone   = s.status === 'completed' || s.status === 'completed_with_errors';
    if (scanActive || scanDone) {
      const completed = (s.processed || 0) + (s.skipped || 0);
      const pct = s.total > 0 ? Math.round((completed / s.total) * 100) : null;
      procs.push({
        id: 'scan',
        phase: 'scan',
        status: scanActive ? 'running' : (s.status === 'completed_with_errors' ? 'error' : 'done'),
        label: scanActive ? 'Indexing workspace' : 'Index complete',
        detail: '',
        progress: pct,
        stats: {
          Files: s.total > 0 ? `${completed}/${s.total}` : '—',
          Skipped: String(s.skipped || 0),
          ...(s.errors > 0 ? { Errors: String(s.errors) } : {}),
        },
      });
    }

    const enrStatus = s.enrichmentStatus;
    const enrActive  = enrStatus === 'running' || enrStatus === 'queued';
    const enrAgg     = enrStatus === 'aggregating';
    const enrDone    = enrStatus === 'done';
    const enrPending = enrStatus === 'pending_confirmation';
    if (enrActive || enrAgg || enrDone || enrPending) {
      const durSec = s.enrichmentDurationMs > 0
        ? (s.enrichmentDurationMs / 1000).toFixed(1) + 's'
        : '—';
      procs.push({
        id: 'enrichment',
        phase: 'enrichment',
        status: enrPending ? 'pending_confirmation' : ((enrActive || enrAgg) ? 'running' : 'done'),
        label: enrPending
          ? `Ready to build AI knowledge for ${s.enrichmentRemaining || 0} file${s.enrichmentRemaining === 1 ? '' : 's'}`
          : (enrAgg ? 'Building module summaries' : (enrDone ? 'AI knowledge ready' : 'Enriching AI knowledge')),
        detail: s.enrichmentPath || '',
        progress: null,     // indeterminate
        stats: enrPending ? {} : {
          Remaining: enrActive ? String(s.enrichmentRemaining || 0) : '—',
          Embeddings: String(s.enrichmentEmbedded || 0),
          Summaries:  String(s.enrichmentSummaries || 0),
          Time: durSec,
        },
      });
    }

    return procs;
  })();

  const value = {
    themeID, themeSettingsOpen, setThemeSettingsOpen, selectTheme,
    fontSettingsOpen, setFontSettingsOpen, fontSettings, updateFontSettings,
    editorSettings, updateEditorSettings,
    activeTheme, studioStyle, studioClassName, antTheme,
    workspacePickerMode, setWorkspacePickerMode, handleWorkspacePickerSelect,
    projects, projectsLoaded, activeProject, selectProject, switchToProject,
    createProjectFromPath, openFileFromPath,
    handleCreateProject, handleOpenFile,
    requestedSessionId, aiPreview, setAiPreview,
    layout, toggleLayout,
    indexStatus, setIndexStatus, scanNotificationMinimized, setScanNotificationMinimized,
    stopIndexing,
    approveEnrichment,
    indexProcesses,
    editorCtx,
    openFiles, activeTab, refreshTrigger, setRefreshTrigger,
    openFile, openFileFromRoute, handleTabChange, handleTabEdit,
    handleFileRenamed, handleFileDeleted, updateCursorAndSync,
    sidebarWidth, chatWidth, handleSidebarDrag, handleChatDrag, sidebarStartRef, chatStartRef,
    handleOpenConversation, handleNavigate,
  };

  if (!settingsLoaded) {
    return (
      <div style={{ height: '100vh', width: '100vw', display: 'grid', placeItems: 'center', background: 'var(--studio-bg, #f7f4ed)' }}>
        <div style={{ color: 'var(--studio-muted, #999)' }}>Loading configurations...</div>
      </div>
    );
  }

  return (
    <AppStateContext.Provider value={value}>
      {children}
    </AppStateContext.Provider>
  );
}
