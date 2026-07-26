import React, { useEffect, useState } from 'react';
import { Table, Typography, Empty, Tag } from 'antd';
import { DatabaseOutlined } from '@ant-design/icons';

const { Title } = Typography;

const PAGE_SIZE = 100;

// Renders one open table's data + paging as its own tab body, so each open
// tab tracks its own page/loading state independently — switching tabs
// doesn't reset or share paging with any other open table.
export default function DatabaseTableTab({ projectId, table }) {
  const [tableData, setTableData] = useState({ columns: [], rows: [], total_rows: 0 });
  const [page, setPage] = useState(1);
  const [loadedKey, setLoadedKey] = useState('');

  const requestKey = `${table}:${page}`;
  const loading = loadedKey !== requestKey;

  useEffect(() => {
    if (!projectId || !table) return;
    let cancelled = false;
    const offset = (page - 1) * PAGE_SIZE;
    fetch(`/api/projects/${projectId}/db/tables/${table}/data?limit=${PAGE_SIZE}&offset=${offset}`)
      .then(res => res.json())
      .then(data => {
        if (cancelled) return;
        setTableData(data || { columns: [], rows: [], total_rows: 0 });
        setLoadedKey(`${table}:${page}`);
      })
      .catch(() => {
        if (cancelled) return;
        setTableData({ columns: [], rows: [], total_rows: 0 });
        setLoadedKey(`${table}:${page}`);
      });
    return () => { cancelled = true; };
  }, [projectId, table, page]);

  // Switching which table this tab shows isn't a prop this component
  // re-mounts for (DatabasePage gives each table its own stable tab key), so
  // there's no cross-table page reset to worry about here.

  const columns = (tableData.columns || []).map(col => ({
    title: col.name,
    dataIndex: col.name,
    key: col.name,
    ellipsis: true,
    render: text => (typeof text === 'object' ? JSON.stringify(text) : String(text ?? '')),
  }));

  return (
    <div style={{ display: 'flex', flexDirection: 'column', height: '100%', gap: 16 }}>
      <div style={{ display: 'flex', gap: 20 }}>
        <div className="db-metric-card">
          <span className="db-metric-title">Rows</span>
          <span className="db-metric-value">{(tableData.total_rows ?? 0).toLocaleString()}</span>
        </div>
        <div className="db-metric-card">
          <span className="db-metric-title">Columns</span>
          <span className="db-metric-value">{tableData.columns?.length || 0}</span>
        </div>
      </div>

      <div className="db-premium-table" style={{ flex: 1, display: 'flex', flexDirection: 'column', minHeight: 0 }}>
        <div style={{ padding: '16px 24px', borderBottom: '1px solid var(--studio-border, #e2dcd4)', display: 'flex', alignItems: 'center', gap: 8, background: 'var(--studio-surface, #fffdf8)', borderTopLeftRadius: 12, borderTopRightRadius: 12 }}>
          <DatabaseOutlined style={{ fontSize: 18, color: 'var(--studio-accent, #c15f3c)' }} />
          <Title level={5} style={{ margin: 0, color: 'var(--studio-text, #2f2a26)' }}>{table}</Title>
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
          scroll={{ x: 'max-content', y: 'calc(100vh - 440px)' }}
          locale={{ emptyText: <Empty description="This table has no rows." /> }}
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
    </div>
  );
}
