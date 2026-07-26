import React, { useState, useEffect } from 'react';
import { useNavigate } from 'react-router-dom';
import { Table, Layout, Typography, Button, Empty, Spin, Tag, Input } from 'antd';
import { ArrowLeftOutlined, DatabaseOutlined, TableOutlined, SearchOutlined } from '@ant-design/icons';
import TopNavbar from '../components/TopNavbar';
import StatusBar from '../components/StatusBar';
import { useAppState } from '../state/useAppState';
import { folderNameFromPath } from '../state/folderNameFromPath';
import { useProjectRouteSync } from '../hooks/useRouteSync';

const { Sider, Content } = Layout;
const { Text, Title } = Typography;

const PAGE_SIZE = 100;

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
  // from a database that genuinely has no tables. Deriving the flag this way
  // (rather than a separate setState at the top of the effect) avoids the
  // cascading re-render that calling setState synchronously in an effect
  // causes.
  const [tables, setTables] = useState(null);
  const [activeTable, setActiveTable] = useState(null);
  const [tableFilter, setTableFilter] = useState('');
  const [tableData, setTableData] = useState({ columns: [], rows: [], total_rows: 0 });
  const [page, setPage] = useState(1);
  // loadedKey records which table+page the data in state belongs to, so
  // "loading" is derived by comparing it against what's currently requested.
  const [loadedKey, setLoadedKey] = useState('');

  const tablesLoading = tables === null;
  const requestKey = activeTable ? `${activeTable}:${page}` : '';
  const loading = Boolean(activeTable) && loadedKey !== requestKey;

  useEffect(() => {
    if (!activeProject?.id) return;
    let cancelled = false;
    fetch(`/api/projects/${activeProject.id}/db/tables`)
      .then(res => res.json())
      .then(data => {
        if (cancelled) return;
        const names = Array.isArray(data) ? data : [];
        setTables(names);
        if (names.length > 0) setActiveTable(current => current || names[0]);
      })
      .catch(() => { if (!cancelled) setTables([]); });
    return () => { cancelled = true; };
  }, [activeProject?.id]);

  useEffect(() => {
    if (!activeProject?.id || !activeTable) return;
    let cancelled = false;
    const offset = (page - 1) * PAGE_SIZE;
    fetch(`/api/projects/${activeProject.id}/db/tables/${activeTable}/data?limit=${PAGE_SIZE}&offset=${offset}`)
      .then(res => res.json())
      .then(data => {
        if (cancelled) return;
        setTableData(data || { columns: [], rows: [], total_rows: 0 });
        setLoadedKey(`${activeTable}:${page}`);
      })
      .catch(() => {
        if (cancelled) return;
        setTableData({ columns: [], rows: [], total_rows: 0 });
        setLoadedKey(`${activeTable}:${page}`);
      });
    return () => { cancelled = true; };
  }, [activeProject?.id, activeTable, page]);

  // Switching tables resets paging here, in the event handler rather than an
  // effect — otherwise an offset from a large table carries over and a
  // smaller table renders as empty.
  const selectTable = name => {
    setActiveTable(name);
    setPage(1);
  };

  // Hard refresh / deep link lands here before the project has resolved.
  // Rendering nothing left a blank white screen with no explanation, which
  // is what made this page look broken on load.
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

  const columns = (tableData.columns || []).map(col => ({
    title: col.name,
    dataIndex: col.name,
    key: col.name,
    ellipsis: true,
    render: text => (typeof text === 'object' ? JSON.stringify(text) : String(text ?? '')),
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
                <Text style={{ color: 'var(--studio-muted, #746b63)', fontSize: 12 }}>SQLite Explorer</Text>
              </div>
            </div>
            <div style={{ padding: '12px 4px', display: 'flex', flexDirection: 'column' }}>
              <Text style={{ padding: '0 16px', marginBottom: 8, fontSize: 11, fontWeight: 600, color: 'var(--studio-muted, #746b63)', textTransform: 'uppercase', letterSpacing: 0.5 }}>
                Tables {(tables || []).length > 0 && `(${tables.length})`}
              </Text>
              {/* This workspace has 30+ tables; scrolling a flat list to find
                  one is why the sidebar felt unusable. */}
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
                  className={`db-nav-item ${activeTable === t ? 'active' : ''}`}
                  onClick={() => selectTable(t)}
                >
                  <TableOutlined /> {t}
                </div>
              ))}
            </div>
          </Sider>
          <Content style={{ padding: '32px 40px', overflowY: 'auto', display: 'flex', flexDirection: 'column', background: 'var(--studio-bg, #f7f4ed)' }}>
            
            <div className="db-animate-in" style={{ marginBottom: 32, display: 'flex', gap: 20 }}>
              <div className="db-metric-card">
                <span className="db-metric-title">Total Tables</span>
                <span className="db-metric-value">{(tables || []).length}</span>
              </div>
              <div className="db-metric-card" style={{ animationDelay: '0.1s' }}>
                {/* Previously showed the fetched page size and called it the
                    row count, so a 27,936-row table reported "100". */}
                <span className="db-metric-title">Rows in Table</span>
                <span className="db-metric-value">{(tableData.total_rows ?? 0).toLocaleString()}</span>
              </div>
              <div className="db-metric-card" style={{ animationDelay: '0.2s' }}>
                <span className="db-metric-title">Columns (Current)</span>
                <span className="db-metric-value">{tableData.columns?.length || 0}</span>
              </div>
            </div>
            
            <div className="db-premium-table db-animate-in" style={{ animationDelay: '0.3s', flex: 1, display: 'flex', flexDirection: 'column', minHeight: 0 }}>
              <div style={{ padding: '16px 24px', borderBottom: '1px solid var(--studio-border, #e2dcd4)', display: 'flex', alignItems: 'center', gap: 8, background: 'var(--studio-surface, #fffdf8)', borderTopLeftRadius: 12, borderTopRightRadius: 12 }}>
                <DatabaseOutlined style={{ fontSize: 18, color: 'var(--studio-accent, #c15f3c)' }} />
                <Title level={5} style={{ margin: 0, color: 'var(--studio-text, #2f2a26)' }}>{activeTable || 'Select a table'}</Title>
                {tableData.total_rows > PAGE_SIZE && (
                  <Tag style={{ marginInlineStart: 4 }}>
                    showing {((page - 1) * PAGE_SIZE + 1).toLocaleString()}–
                    {Math.min(page * PAGE_SIZE, tableData.total_rows).toLocaleString()} of {tableData.total_rows.toLocaleString()}
                  </Tag>
                )}
              </div>
              <Table
                dataSource={tableData.rows}
                columns={columns}
                rowKey={(record, idx) => record.id || record.uuid || record.path || idx}
                loading={loading}
                scroll={{ x: 'max-content', y: 'calc(100vh - 400px)' }}
                locale={{
                  emptyText: <Empty description={activeTable ? 'This table has no rows.' : 'Select a table to view its data.'} />,
                }}
                // Server-side paging: the backend returns one page at a time,
                // so total comes from the row count rather than what's loaded.
                pagination={{
                  current: page,
                  pageSize: PAGE_SIZE,
                  total: tableData.total_rows || 0,
                  onChange: setPage,
                  showSizeChanger: false,
                  size: 'small',
                  style: { padding: '10px 16px', margin: 0 },
                  showTotal: (total, range) => `${range[0].toLocaleString()}–${range[1].toLocaleString()} of ${total.toLocaleString()}`,
                }}
                size="middle"
                style={{ flex: 1 }}
              />
            </div>
          </Content>
        </Layout>
      </div>

      <StatusBar indexStatus={indexStatus} scanMinimized={scanNotificationMinimized} onScanMinimize={() => setScanNotificationMinimized(true)} onScanExpand={() => setScanNotificationMinimized(false)} />
    </div>
  );
}
