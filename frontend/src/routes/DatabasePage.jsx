import React, { useState, useEffect } from 'react';
import { useNavigate } from 'react-router-dom';
import { Table, Layout, Menu, Typography, Button } from 'antd';
import { ArrowLeftOutlined, DatabaseOutlined, TableOutlined } from '@ant-design/icons';
import TopNavbar from '../components/TopNavbar';
import StatusBar from '../components/StatusBar';
import IndexActivityWidget from '../components/IndexActivityWidget';
import { useAppState } from '../state/useAppState';
import { folderNameFromPath } from '../state/folderNameFromPath';
import { useProjectRouteSync } from '../hooks/useRouteSync';

const { Sider, Content } = Layout;
const { Text, Title } = Typography;

export default function DatabasePage() {
  const navigate = useNavigate();
  const projectId = useProjectRouteSync();
  const {
    studioStyle, studioClassName, activeProject,
    handleNavigate, handleOpenFile, handleCreateProject,
    setScanNotificationMinimized,
    indexStatus, scanNotificationMinimized, indexProcesses,
  } = useAppState();

  const [tables, setTables] = useState([]);
  const [activeTable, setActiveTable] = useState(null);
  const [tableData, setTableData] = useState({ columns: [], rows: [] });
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    if (!activeProject?.id) return;
    fetch(`/api/projects/${activeProject.id}/db/tables`)
      .then(res => res.json())
      .then(data => {
        setTables(data || []);
        if (data && data.length > 0) setActiveTable(data[0]);
      })
      .catch(console.error);
  }, [activeProject?.id]);

  useEffect(() => {
    if (!activeProject?.id || !activeTable) return;
    setLoading(true);
    fetch(`/api/projects/${activeProject.id}/db/tables/${activeTable}/data`)
      .then(res => res.json())
      .then(data => {
        setTableData(data || { columns: [], rows: [] });
        setLoading(false);
      })
      .catch(err => {
        console.error(err);
        setLoading(false);
      });
  }, [activeProject?.id, activeTable]);

  if (!activeProject || activeProject.id !== projectId) return null;

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
              <Text style={{ padding: '0 16px', marginBottom: 8, fontSize: 11, fontWeight: 600, color: 'var(--studio-muted, #746b63)', textTransform: 'uppercase', letterSpacing: 0.5 }}>Tables</Text>
              {tables.map(t => (
                <div
                  key={t}
                  className={`db-nav-item ${activeTable === t ? 'active' : ''}`}
                  onClick={() => setActiveTable(t)}
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
                <span className="db-metric-value">{tables.length}</span>
              </div>
              <div className="db-metric-card" style={{ animationDelay: '0.1s' }}>
                <span className="db-metric-title">Rows (Current)</span>
                <span className="db-metric-value">{tableData.rows?.length || 0}</span>
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
              </div>
              <Table 
                dataSource={tableData.rows} 
                columns={columns} 
                rowKey={(record, idx) => record.id || record.uuid || record.path || idx}
                loading={loading}
                scroll={{ x: 'max-content', y: 'calc(100vh - 360px)' }}
                pagination={false}
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
