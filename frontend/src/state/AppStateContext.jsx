import React, { useState, useEffect, useRef, useCallback } from 'react';
import { useNavigate } from 'react-router-dom';
import { theme as antdTheme } from 'antd';
import { useEditorContext } from '../useEditorContext';
import { getStudioTheme, themeVariables } from '../themes';
import { fileURL } from './fileURL';
import { folderNameFromPath } from './folderNameFromPath';
import { AppStateContext } from './context';
import { loadFontSettings, saveFontSettings } from './fontSettings';

export function AppStateProvider({ children }) {
  const navigate = useNavigate();

  const [themeID, setThemeID] = useState(() => window.localStorage.getItem('nativestudio.theme') || 'linear');
  const [themeSettingsOpen, setThemeSettingsOpen] = useState(false);
  const [fontSettingsOpen, setFontSettingsOpen] = useState(false);
  const [fontSettings, setFontSettings] = useState(loadFontSettings);
  const [workspacePickerMode, setWorkspacePickerMode] = useState(null);
  const [projects, setProjects] = useState([]);
  const [projectsLoaded, setProjectsLoaded] = useState(false);
  const [activeProject, setActiveProject] = useState(null);
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

  const updateFontSettings = patch => {
    setFontSettings(prev => {
      const next = { ...prev, ...patch };
      saveFontSettings(next);
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

  // selectProject + navigate to the project's base URL — use this for any
  // user-initiated project switch (as opposed to the route-sync hook, which
  // calls selectProject directly since the URL is already where it should be).
  const switchToProject = useCallback(async (proj) => {
    await selectProject(proj);
    navigate(`/projects/${proj.id}`);
  }, [navigate]);

  const handleCreateProject = () => setWorkspacePickerMode('folder');
  const handleOpenFile = () => setWorkspacePickerMode('file');

  const createProjectFromPath = async path => {
    // Opening a folder that's already a known project must switch to it
    // instead of trying (and failing) to register a duplicate.
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

  // ── File tabs, kept in sync with the URL ─────────────────────────────
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
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // User-initiated file open (tree click, chat "open file", knowledge page) —
  // updates the URL too, so the open file is deep-linkable/restorable.
  const openFile = useCallback((path) => {
    openFileLocal(path);
    if (activeProject) navigate(fileURL(activeProject.id, path));
  }, [openFileLocal, activeProject, navigate]);

  // Called by the route-sync hook when the URL names a file that isn't open
  // yet. Must not re-navigate — the URL is already the source of truth here.
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

  // Keep open editor tabs (and the URL) in sync when the file tree renames or
  // deletes something out from under them — otherwise a stale tab/URL would
  // keep pointing at a path that no longer exists.
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

  // Cursor position is written into the URL (?line=&col=) so the AI's editor
  // context and a deep link both reflect exactly where the user is — debounced
  // and history-replacing since it fires on every cursor move.
  //
  // Reads window.location directly (not the useLocation()/useSearchParams()
  // hook values) because this provider sits above <Routes>, and by the time
  // the debounce timer fires, this component's own hook-derived `location`
  // has been observed to still report the pre-navigation pathname (the
  // /files/* segment missing) even though the browser's actual URL is
  // already correct — reading the DOM's live location sidesteps that
  // staleness entirely and is always accurate.
  const cursorSyncTimer = useRef(null);
  const updateCursorAndSync = (line, column) => {
    editorCtx.updateCursor(line, column);
    if (cursorSyncTimer.current) window.clearTimeout(cursorSyncTimer.current);
    cursorSyncTimer.current = window.setTimeout(() => {
      const next = new URLSearchParams(window.location.search);
      next.set('line', String(line));
      next.set('col', String(column));
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
  };

  const value = {
    themeID, themeSettingsOpen, setThemeSettingsOpen, selectTheme,
    fontSettingsOpen, setFontSettingsOpen, fontSettings, updateFontSettings,
    activeTheme, studioStyle, studioClassName, antTheme,
    workspacePickerMode, setWorkspacePickerMode, handleWorkspacePickerSelect,
    projects, projectsLoaded, activeProject, selectProject, switchToProject,
    createProjectFromPath, openFileFromPath,
    handleCreateProject, handleOpenFile,
    requestedSessionId, aiPreview, setAiPreview,
    layout, toggleLayout,
    indexStatus, setIndexStatus, scanNotificationMinimized, setScanNotificationMinimized,
    editorCtx,
    openFiles, activeTab, refreshTrigger, setRefreshTrigger,
    openFile, openFileFromRoute, handleTabChange, handleTabEdit,
    handleFileRenamed, handleFileDeleted, updateCursorAndSync,
    sidebarWidth, chatWidth, handleSidebarDrag, handleChatDrag, sidebarStartRef, chatStartRef,
    handleOpenConversation, handleNavigate,
  };

  return <AppStateContext.Provider value={value}>{children}</AppStateContext.Provider>;
}
