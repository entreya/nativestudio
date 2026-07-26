import React, { useState, useEffect } from 'react';
import { useNavigate } from 'react-router-dom';
import { Layout, Typography, Button, Spin, Input, Tabs, Empty } from 'antd';
import { ArrowLeftOutlined, DatabaseOutlined, TableOutlined, SearchOutlined, ApartmentOutlined, PlusOutlined } from '@ant-design/icons';
import TopNavbar from '../components/TopNavbar';
import StatusBar from '../components/StatusBar';
import DatabaseTableTab from '../components/DatabaseTableTab';
import QueryBuilderTab from '../components/QueryBuilderTab';
import { useAppState } from '../state/useAppState';
import { folderNameFromPath } from '../state/folderNameFromPath';
import { useProjectRouteSync } from '../hooks/useRouteSync';

const { Sider, Content } = Layout;
const { Text } = Typography;

let nextQueryTabId = 1;

export default function DatabasePage() {
  const navigate = useNavigate();
  const projectId = useProjectRouteSync();
  const {
    studioStyle, studioClassName, activeProject,
    handleNavigate, handleOpenFile, handleCreateProject,
    setScanNotificationMinimized,
    indexStatus, scanNotificationMinimized,
  } = useAppState();

  // `tables === null` means "not fetched yet", which distinguishes loading
  // from a database that genuinely has no tables.
  const [tables, setTables] = useState(null);
  const [tableFilter, setTableFilter] = useState('');
  // Each open table/query gets its own tab — {key, type: 'table'|'query', table?}.
  // Only opened tabs mount a DatabaseTableTab/QueryBuilderTab, and each one
  // owns its own paging/query state independently of the others.
  const [openTabs, setOpenTabs] = useState([]);
  const [activeTabKey, setActiveTabKey] = useState('');

  const tablesLoading = tables === null;

  const openTable = (name) => {
    const key = `table:${name}`;
    setOpenTabs(prev => (prev.some(t => t.key === key) ? prev : [...prev, { key, type: 'table', table: name }]));
    setActiveTabKey(key);
  };

  const openNewQuery = () => {
    const key = `query:${nextQueryTabId++}`;
    setOpenTabs(prev => [...prev, { key, type: 'query' }]);
    setActiveTabKey(key);
  };

  const closeTab = (targetKey) => {
    setOpenTabs(prev => {
      const index = prev.findIndex(t => t.key === targetKey);
      const next = prev.filter(t => t.key !== targetKey);
      if (activeTabKey === targetKey && next.length > 0) {
        const neighbor = next[Math.max(0, index - 1)] || next[0];
        setActiveTabKey(neighbor.key);
      } else if (next.length === 0) {
        setActiveTabKey('');
      }
      return next;
    });
  };

  const handleTabEdit = (targetKey, action) => {
    if (action === 'remove') closeTab(targetKey);
  };

  useEffect(() => {
    if (!activeProject?.id) return;
    let cancelled = false;
    fetch(`/api/projects/${activeProject.id}/db/tables`)
      .then(res => res.json())
      .then(data => {
        if (cancelled) return;
        const names = Array.isArray(data) ? data : [];
        setTables(names);
        // Open the first table automatically so a fresh visit isn't a bare
        // empty state — inside this .then rather than a separate effect
        // reacting to `tables`, so it only ever fires once per load of this
        // project (this fetch itself only runs once per activeProject.id),
        // with no extra ref needed to guard against re-firing later when the
        // user closes every tab.
        if (names.length > 0) openTable(names[0]);
      })
      .catch(() => { if (!cancelled) setTables([]); });
    return () => { cancelled = true; };
  }, [activeProject?.id]);

  // Hard refresh / deep link lands here before the project has resolved.
  if (!activeProject || activeProject.id !== projectId) {
    return (
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'center', height: '100vh', gap: 12 }}>
        <Spin />
        <Text type="secondary">Loading workspace…</Text>
      </div>
    );
  }

  const visibleTables = (tables || []).filter(name =>
    name.toLowerCase().includes(tableFilter.trim().toLowerCase()));

  const tabItems = openTabs.map(tab => ({
    key: tab.key,
    label: tab.type === 'query'
      ? <span><ApartmentOutlined style={{ marginInlineEnd: 6 }} />Query</span>
      : <span><TableOutlined style={{ marginInlineEnd: 6 }} />{tab.table}</span>,
    children: tab.type === 'query'
      ? <QueryBuilderTab projectId={activeProject.id} tables={tables || []} />
      : <DatabaseTableTab projectId={activeProject.id} table={tab.table} />,
  }));

  return (
    <div className={studioClassName} style={{ ...studioStyle, display: 'flex', flexDirection: 'column', width: '100vw', height: '100vh', overflow: 'hidden', background: 'var(--studio-bg, #f7f4ed)' }}>
      <TopNavbar folderName={folderNameFromPath(activeProject.path)} onNavigate={handleNavigate} onOpenFile={handleOpenFile} onOpenFolder={handleCreateProject} />

      <div style={{ flex: 1, display: 'flex', overflow: 'hidden' }}>
        <Layout style={{ background: 'transparent' }}>
          <Sider width={260} style={{ background: 'var(--studio-panel, #eee9df)', borderRight: '1px solid var(--studio-border, #d8d1c5)', overflowY: 'auto' }}>
            <div style={{ padding: '20px 16px', borderBottom: '1px solid var(--studio-border, #d8d1c5)', display: 'flex', alignItems: 'center', gap: 12 }}>
              <Button type="text" icon={<ArrowLeftOutlined />} onClick={() => navigate(`/projects/${activeProject.id}`)} />
              <div style={{ display: 'flex', flexDirection: 'column' }}>
                <Text strong style={{ color: 'var(--studio-text, #2f2a26)', fontSize: 15 }}>Database Dashboard</Text>
                <Text style={{ color: 'var(--studio-muted, #746b63)', fontSize: 12 }}>
                  {tablesLoading ? 'SQLite Explorer' : `${(tables || []).length} table${(tables || []).length === 1 ? '' : 's'}`}
                </Text>
              </div>
            </div>

            <div style={{ padding: '14px 12px' }}>
              <Button block type="dashed" icon={<ApartmentOutlined />} onClick={openNewQuery}>
                New Query
              </Button>
            </div>

            <div style={{ padding: '0 4px 12px', display: 'flex', flexDirection: 'column' }}>
              <Text style={{ padding: '0 16px', marginBottom: 8, fontSize: 11, fontWeight: 600, color: 'var(--studio-muted, #746b63)', textTransform: 'uppercase', letterSpacing: 0.5 }}>
                Tables
              </Text>
              {(tables || []).length > 8 && (
                <Input
                  size="small"
                  allowClear
                  prefix={<SearchOutlined style={{ color: 'var(--studio-subtle, #91877e)' }} />}
                  placeholder="Filter tables"
                  value={tableFilter}
                  onChange={event => setTableFilter(event.target.value)}
                  style={{ margin: '0 12px 10px' }}
                />
              )}
              {tablesLoading ? (
                <div style={{ padding: '16px', textAlign: 'center' }}><Spin size="small" /></div>
              ) : visibleTables.length === 0 ? (
                <Text style={{ padding: '8px 16px', fontSize: 12, color: 'var(--studio-muted, #746b63)' }}>
                  {(tables || []).length === 0 ? "No tables in this database." : "No tables match that filter."}
                </Text>
              ) : visibleTables.map(t => (
                <div
                  key={t}
                  className={`db-nav-item ${activeTabKey === `table:${t}` ? 'active' : ''}`}
                  onClick={() => openTable(t)}
                >
                  <TableOutlined /> {t}
                </div>
              ))}
            </div>
          </Sider>

          <Content style={{ padding: '20px 28px', overflow: 'hidden', display: 'flex', flexDirection: 'column', background: 'var(--studio-bg, #f7f4ed)' }}>
            {tabItems.length === 0 ? (
              <div style={{ flex: 1, display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
                <Empty
                  image={<DatabaseOutlined style={{ fontSize: 48, color: 'var(--studio-subtle, #91877e)' }} />}
                  description={
                    <div>
                      <div style={{ marginBottom: 12 }}>Select a table from the sidebar, or build a query across several.</div>
                      <Button icon={<PlusOutlined />} onClick={openNewQuery}>New Query</Button>
                    </div>
                  }
                />
              </div>
            ) : (
              <Tabs
                type="editable-card"
                hideAdd
                activeKey={activeTabKey}
                onChange={setActiveTabKey}
                onEdit={handleTabEdit}
                items={tabItems}
                style={{ flex: 1, minHeight: 0, display: 'flex', flexDirection: 'column' }}
                className="db-tabs"
              />
            )}
          </Content>
        </Layout>
      </div>

      <StatusBar indexStatus={indexStatus} scanMinimized={scanNotificationMinimized} onScanMinimize={() => setScanNotificationMinimized(true)} onScanExpand={() => setScanNotificationMinimized(false)} />
    </div>
  );
}
