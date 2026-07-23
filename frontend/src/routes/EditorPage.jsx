import React from 'react';
import { Typography, Button, Tabs, Tooltip, Avatar, Breadcrumb, Space, Dropdown } from 'antd';
import {
  FolderOpenOutlined, FileOutlined, SettingOutlined, SearchOutlined,
  AppstoreOutlined, UserOutlined, BranchesOutlined, PlusOutlined,
  EllipsisOutlined, CodeOutlined, DatabaseOutlined, BgColorsOutlined, FontSizeOutlined,
} from '@ant-design/icons';
import { useNavigate } from 'react-router-dom';
import FileTree from '../components/FileTree';
import EditorPanel from '../components/EditorPanel';
import ChatPanel from '../components/ChatPanel';
import TopNavbar from '../components/TopNavbar';
import StatusBar from '../components/StatusBar';
import DragHandle from '../components/DragHandle';
import ThemeSettingsModal from '../components/ThemeSettingsModal';
import FontSettingsModal from '../components/FontSettingsModal';
import { useAppState } from '../state/useAppState';
import { folderNameFromPath } from '../state/folderNameFromPath';
import { useProjectRouteSync, useFileRouteSync } from '../hooks/useRouteSync';

const { Text } = Typography;

const settingsMenuItems = [
  { key: 'theme', icon: <BgColorsOutlined />, label: 'Theme' },
  { key: 'fonts', icon: <FontSizeOutlined />, label: 'Fonts' },
];

export default function EditorPage() {
  const navigate = useNavigate();
  const projectId = useProjectRouteSync();
  useFileRouteSync(projectId);

  const {
    studioStyle, studioClassName, activeTheme,
    themeSettingsOpen, setThemeSettingsOpen, themeID, selectTheme,
    fontSettingsOpen, setFontSettingsOpen, fontSettings, updateFontSettings,
    activeProject, handleNavigate, handleOpenFile, handleCreateProject,
    layout, toggleLayout,
    sidebarWidth, chatWidth, handleSidebarDrag, handleChatDrag, sidebarStartRef, chatStartRef,
    editorCtx, openFiles, activeTab, refreshTrigger, setRefreshTrigger,
    openFile, handleTabChange, handleTabEdit, handleFileRenamed, handleFileDeleted, updateCursorAndSync,
    aiPreview, setAiPreview,
    requestedSessionId,
    indexStatus, scanNotificationMinimized, setScanNotificationMinimized,
  } = useAppState();

  if (!activeProject || activeProject.id !== projectId) return null;

  const folderName = folderNameFromPath(activeProject.path);
  const breadcrumbItems = activeTab
    ? [folderName, ...activeTab.split(/[\\/]/).filter(Boolean)].map((title, index, items) => ({
        title: index === items.length - 1
          ? <Text style={{ color: 'var(--studio-text, #2f2a26)', fontSize: 12 }}>{title}</Text>
          : title,
      }))
    : [];

  return (
    <div className={studioClassName} style={{ ...studioStyle, display: 'flex', flexDirection: 'column', height: '100vh', width: '100vw', overflow: 'hidden', backgroundColor: 'var(--studio-bg, #f7f4ed)', color: 'var(--studio-text, #2f2a26)' }}>

      <TopNavbar
        folderName={folderName}
        onNavigate={handleNavigate}
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
              <DatabaseOutlined onClick={() => navigate(`/projects/${activeProject.id}/knowledge`)} style={{ fontSize: 24, color: 'var(--studio-subtle, #91877e)', cursor: 'pointer' }} />
            </Tooltip>
          </div>

          <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', gap: 24, width: '100%', paddingBottom: 8 }}>
            <Tooltip placement="right" title="Settings">
              <Dropdown
                trigger={['click']}
                placement="rightBottom"
                menu={{
                  items: settingsMenuItems,
                  onClick: ({ key }) => {
                    if (key === 'theme') setThemeSettingsOpen(true);
                    else if (key === 'fonts') setFontSettingsOpen(true);
                  },
                }}
              >
                <span style={{ display: 'inline-flex', cursor: 'pointer' }}>
                  <SettingOutlined style={{ fontSize: 24, color: 'var(--studio-subtle, #91877e)' }} />
                </span>
              </Dropdown>
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

            <div style={{
              display: 'flex', alignItems: 'center', padding: '4px 16px',
              fontSize: '11px', fontWeight: 600, color: 'var(--studio-text, #2f2a26)', textTransform: 'uppercase', cursor: 'pointer'
            }}>
              <FolderOpenOutlined style={{ marginRight: 6 }} /> {folderName}
            </div>

            <div style={{ flex: 1, overflow: 'hidden' }}>
              <FileTree onFileClick={openFile} refreshTrigger={refreshTrigger} onFileRenamed={handleFileRenamed} onFileDeleted={handleFileDeleted} />
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

          {activeTab && (
            <div style={{ minHeight: 29, padding: '4px 16px', overflowX: 'auto', backgroundColor: 'var(--studio-bg, #f7f4ed)', borderBottom: '1px solid var(--studio-border, #d8d1c5)' }}>
              <Breadcrumb separator="›" items={breadcrumbItems} style={{ whiteSpace: 'nowrap', fontSize: 12 }} />
            </div>
          )}

          <div style={{ flex: 1, overflow: 'hidden', position: 'relative' }}>
            {activeTab ? (
              <EditorPanel
                filepath={activeTab}
                darkMode={activeTheme.dark}
                aiPreview={aiPreview}
                fontSettings={fontSettings}
                initialCursor={editorCtx.context.cursor}
                onCursorChange={updateCursorAndSync}
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
            onOpenFile={openFile}
            onPreviewChange={setAiPreview}
            onPreviewClear={() => setAiPreview(null)}
            onFilesChanged={() => setRefreshTrigger(prev => prev + 1)}
          />
        </div>}

      </div>

      <StatusBar indexStatus={indexStatus} scanMinimized={scanNotificationMinimized} onScanMinimize={() => setScanNotificationMinimized(true)} onScanExpand={() => setScanNotificationMinimized(false)} />

      <ThemeSettingsModal open={themeSettingsOpen} selectedTheme={themeID} onSelect={selectTheme} onClose={() => setThemeSettingsOpen(false)} />
      <FontSettingsModal open={fontSettingsOpen} settings={fontSettings} onChange={updateFontSettings} onClose={() => setFontSettingsOpen(false)} />
    </div>
  );
}
