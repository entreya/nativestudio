import React, { useEffect, useMemo, useState } from 'react';
import { Alert, Button, Card, Empty, Input, InputNumber, List, Modal, Popconfirm, Progress, Space, Spin, Statistic, Tag, Typography } from 'antd';
import { CodeOutlined, DatabaseOutlined, DeleteOutlined, FileSearchOutlined, ReloadOutlined } from '@ant-design/icons';

const { Paragraph, Text, Title } = Typography;
const parseSummary = value => { try { return JSON.parse(value || '{}'); } catch { return { architecture: value }; } };
const fetchKnowledge = async projectID => {
  const response = await fetch(`/api/projects/${projectID}/knowledge`);
  const data = await response.json();
  if (!response.ok) throw new Error(data.message || 'Could not load project knowledge');
  return data;
};

export default function ProjectKnowledgePage({ project, onOpenFile, onBack, onRelearnStart }) {
  const projectID = project?.id;
  const [knowledge, setKnowledge] = useState(null);
  const [events, setEvents] = useState([]);
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(true);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [relearning, setRelearning] = useState(false);
  const [settings, setSettings] = useState({ include_paths: [], exclude_paths: [], sensitive_patterns: [], maximum_file_size_bytes: 1048576 });

  useEffect(() => {
    if (!projectID) return undefined;
    let cancelled = false;
    fetchKnowledge(projectID)
      .then(data => { if (!cancelled) { setKnowledge(data); setError(''); } })
      .catch(cause => { if (!cancelled) setError(cause.message); })
      .finally(() => { if (!cancelled) setLoading(false); });
    const source = new EventSource(`/api/projects/${projectID}/index/events`);
    source.onmessage = event => {
      const update = JSON.parse(event.data);
      setEvents(items => [...items.slice(-24), update]);
      if (update.type === 'index_progress') {
        setKnowledge(current => current ? { ...current, index_status: { ...current.index_status, status: 'running', ...update.data } } : current);
      }
      if (update.type === 'enrichment_started' || update.type === 'enrichment_progress' || update.type === 'enrichment_aggregating') {
        setKnowledge(current => current ? { ...current, index_status: { ...current.index_status, enrichment_status: 'running', enrichment_remaining: update.data?.remaining || 0 } } : current);
      }
      if (update.type === 'enrichment_completed') {
        setKnowledge(current => current ? { ...current, index_status: { ...current.index_status, enrichment_status: 'completed', enrichment_remaining: 0 } } : current);
      }
      if (update.type === 'index_completed' || update.type === 'index_error' || update.type === 'enrichment_completed') {
        fetchKnowledge(projectID).then(data => { if (!cancelled) setKnowledge(data); }).catch(() => {});
      }
    };
    return () => { cancelled = true; source.close(); };
  }, [projectID]);

  const projectSummary = useMemo(() => parseSummary(knowledge?.project_summary?.summary), [knowledge]);
  const status = knowledge?.index_status || {};
  const progress = status.total ? Math.round(((status.processed + status.skipped) / status.total) * 100) : 0;
  const reindex = async () => { setEvents([]); await fetch(`/api/projects/${projectID}/index`, { method: 'POST' }); setKnowledge(current => current ? { ...current, index_status: { ...current.index_status, status: 'running', processed: 0 } } : current); };
  const relearn = async () => {
    setRelearning(true);
    setEvents([]);
    try {
      const response = await fetch(`/api/projects/${projectID}/knowledge/relearn`, { method: 'POST' });
      if (!response.ok) throw new Error((await response.text()) || 'Could not reset project knowledge');
      setKnowledge(current => current ? {
        ...current,
        indexed_files: 0,
        indexed_chunks: 0,
        symbol_count: 0,
        stale_count: 0,
        project_summary: null,
        module_summaries: [],
        file_summaries: [],
        facts: [],
        important_symbols: [],
        indexing_errors: [],
        skipped_files: [],
        index_status: { status: 'running', processed: 0, total: 0, skipped: 0, errors: 0 },
      } : current);
      onRelearnStart?.();
    } catch (cause) {
      setError(cause.message);
    } finally {
      setRelearning(false);
    }
  };
  const updateFact = async (fact, changes) => {
    const next = { fact: fact.fact, status: fact.status, pinned: fact.pinned, is_rule: fact.is_rule, ...changes };
    const response = await fetch(`/api/projects/${projectID}/knowledge/facts/${fact.id}`, { method: 'PATCH', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(next) });
    if (response.ok) setKnowledge(current => ({ ...current, facts: current.facts.map(item => item.id === fact.id ? { ...item, ...next } : item) }));
  };
  const correctFact = fact => { const corrected = window.prompt('Correct this project fact', fact.fact); if (corrected?.trim()) updateFact(fact, { fact: corrected.trim(), status: 'verified' }); };
  const deleteFact = async fact => { const response = await fetch(`/api/projects/${projectID}/knowledge/facts/${fact.id}`, { method: 'DELETE' }); if (response.ok) setKnowledge(current => ({ ...current, facts: current.facts.filter(item => item.id !== fact.id) })); };
  const reindexFile = path => fetch(`/api/projects/${projectID}/index/files`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ path }) });
  const openSettings = async () => { const response = await fetch(`/api/projects/${projectID}/knowledge/settings`); if (response.ok) setSettings(await response.json()); setSettingsOpen(true); };
  const saveSettings = async () => { const response = await fetch(`/api/projects/${projectID}/knowledge/settings`, { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(settings) }); if (response.ok) setSettingsOpen(false); };
  const lines = value => value.split(/\r?\n/).map(item => item.trim()).filter(Boolean);

  if (loading) return <main className="memory-page"><div className="memory-loading"><Spin size="large" /></div></main>;
  return (
    <main className="memory-page knowledge-page">
      <section className="memory-hero">
        <div><Text className="eyebrow">LOCAL REPOSITORY INDEX</Text><Title level={1}>Project Knowledge</Title><Paragraph>{project?.path}</Paragraph></div>
        <Space wrap>
          <Button onClick={onBack}>Back to editor</Button>
          <Button onClick={openSettings}>Index settings</Button>
          <Button type="primary" icon={<ReloadOutlined />} onClick={reindex}>Re-index workspace</Button>
          <Popconfirm
            title="Learn this project again?"
            description="This clears generated project knowledge and rebuilds it from the current files. Conversations and decisions are kept."
            okText="Clear and learn again"
            okButtonProps={{ danger: true }}
            onConfirm={relearn}
          >
            <Button danger icon={<DeleteOutlined />} loading={relearning}>Fresh perspective</Button>
          </Popconfirm>
        </Space>
      </section>
      {error && <Alert type="error" showIcon message={error} style={{ maxWidth: 1180, margin: '0 auto 20px' }} />}
      <section className="knowledge-stats">
        <Card><Statistic title="Indexed files" value={knowledge?.indexed_files || 0} prefix={<FileSearchOutlined />} /></Card>
        <Card><Statistic title="Symbols" value={knowledge?.symbol_count || 0} prefix={<CodeOutlined />} /></Card>
        <Card><Statistic title="Chunks" value={knowledge?.indexed_chunks || 0} prefix={<DatabaseOutlined />} /></Card>
        <Card><Statistic title="Stale" value={knowledge?.stale_count || 0} /></Card>
      </section>
      <section className="knowledge-grid">
        <Card title="Indexing status" extra={<Tag color={status.status === 'running' ? 'processing' : status.errors ? 'warning' : 'success'}>{status.status || 'not indexed'}</Tag>}>
          <Progress percent={progress} status={status.errors ? 'exception' : status.status === 'running' ? 'active' : 'normal'} />
          <Text type="secondary">{status.processed || 0} processed · {status.skipped || 0} skipped · {status.errors || 0} errors</Text>
          {status.completed_at && <Paragraph type="secondary" style={{ margin: '6px 0 0' }}>Last completed {new Date(status.completed_at).toLocaleString()}</Paragraph>}
          {events.length > 0 && <List size="small" className="index-events" dataSource={events.slice(-8).reverse()} renderItem={item => <List.Item><Text code>{item.type}</Text><Text ellipsis>{item.data?.path || item.data?.stage || ''}</Text></List.Item>} />}
        </Card>
        <Card title="Skipped files and indexing errors">
          {(knowledge?.indexing_errors || []).length === 0 && (knowledge?.skipped_files || []).length === 0 ? <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="No recent indexing problems" /> : <List size="small" dataSource={[
            ...(knowledge?.indexing_errors || []).map(item => ({ ...item, kind: 'error', detail: `${item.stage}: ${item.error}` })),
            ...(knowledge?.skipped_files || []).map(item => ({ ...item, kind: 'skipped', detail: item.reason })),
          ].slice(0, 30)} renderItem={item => <List.Item><List.Item.Meta title={<Space><Tag color={item.kind === 'error' ? 'red' : 'default'}>{item.kind}</Tag><Text>{item.path || 'Workspace'}</Text></Space>} description={item.detail} /></List.Item>} />}
        </Card>
        <Card title="Detected project">
          {knowledge?.project_summary ? <Space direction="vertical" size={8}>
            <Title level={4} style={{ margin: 0 }}>{projectSummary.project_name || project?.name}</Title>
            <Text>{projectSummary.project_type || 'Project type not established'}</Text>
            <Paragraph>{projectSummary.architecture || 'Architecture summary is not available yet.'}</Paragraph>
            <Space wrap>{[...(projectSummary.languages || []), ...(projectSummary.frameworks || [])].map(value => <Tag key={value}>{value}</Tag>)}</Space>
          </Space> : <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="Project summary will appear after indexing" />}
        </Card>
        <Card title="Languages">
          <Space wrap>{Object.entries(knowledge?.languages || {}).map(([language, count]) => <Tag color="volcano" key={language}>{language} · {count}</Tag>)}</Space>
        </Card>
        <Card title="Modules and features">
          <List size="small" dataSource={knowledge?.module_summaries || []} locale={{ emptyText: 'Module summaries will appear after file summaries are grouped.' }} renderItem={item => { const summary = parseSummary(item.summary); return <List.Item><List.Item.Meta title={item.module_path} description={summary.purpose || item.summary} /></List.Item>; }} />
        </Card>
        <Card title="API routes">
          <List size="small" dataSource={(knowledge?.facts || []).filter(fact => fact.category === 'route' && fact.status === 'verified')} locale={{ emptyText: 'No routes detected yet' }} renderItem={fact => <List.Item actions={fact.sources?.[0] ? [<Button type="link" key="source" onClick={() => onOpenFile(fact.sources[0].path, fact.sources[0])}>Source</Button>] : []}>{fact.fact}</List.Item>} />
        </Card>
        <Card title="Database entities">
          <List size="small" dataSource={(knowledge?.facts || []).filter(fact => ['database', 'database_entity'].includes(fact.category) && fact.status === 'verified')} locale={{ emptyText: 'No database entities detected yet' }} renderItem={fact => <List.Item actions={fact.sources?.[0] ? [<Button type="link" key="source" onClick={() => onOpenFile(fact.sources[0].path, fact.sources[0])}>Source</Button>] : []}>{fact.fact}</List.Item>} />
        </Card>
        <Card title="Important classes and services" className="knowledge-files">
          <List size="small" dataSource={knowledge?.important_symbols || []} locale={{ emptyText: 'No source symbols indexed yet' }} renderItem={symbol => { const summary = parseSummary(symbol.summary); return <List.Item actions={[<Button type="link" key="source" onClick={() => onOpenFile(symbol.path, { start_line: symbol.start_line, end_line: symbol.end_line })}>Source</Button>]}><List.Item.Meta title={<Space><Text strong>{symbol.symbol_name}</Text><Tag>{symbol.symbol_type}</Tag></Space>} description={summary.purpose || symbol.signature} /></List.Item>; }} />
        </Card>
        <Card title="Verified facts" className="knowledge-files">
          <List dataSource={knowledge?.facts || []} locale={{ emptyText: 'Verified source-backed facts will appear after indexing manifests and configuration.' }} renderItem={fact => <List.Item actions={[
            fact.sources?.[0] && <Button type="link" key="source" onClick={() => onOpenFile(fact.sources[0].path, fact.sources[0])}>Source</Button>,
            <Button type="link" key="correct" onClick={() => correctFact(fact)}>Correct</Button>,
            <Button type="link" key="pin" onClick={() => updateFact(fact, { pinned: !fact.pinned })}>{fact.pinned ? 'Unpin' : 'Pin'}</Button>,
            <Button type="link" key="rule" onClick={() => updateFact(fact, { is_rule: !fact.is_rule })}>{fact.is_rule ? 'Remove rule' : 'Make rule'}</Button>,
            <Popconfirm key="delete" title="Delete this fact?" onConfirm={() => deleteFact(fact)}><Button type="link" danger>Delete</Button></Popconfirm>,
          ].filter(Boolean)}><List.Item.Meta title={<Space wrap><Text>{fact.fact}</Text><Tag>{fact.status}</Tag>{fact.pinned && <Tag color="volcano">Pinned</Tag>}{fact.is_rule && <Tag color="blue">Rule</Tag>}</Space>} description={`${fact.category} · ${Math.round((fact.confidence || 0) * 100)}% confidence`} /></List.Item>} />
        </Card>
        <Card title="Accepted and rejected decisions">
          <List size="small" dataSource={knowledge?.decisions || []} locale={{ emptyText: 'No reviewed AI decisions yet' }} renderItem={item => <List.Item><List.Item.Meta title={item.decision} description={<Space><Tag color={item.status === 'accepted' ? 'green' : 'red'}>{item.status}</Tag><Text type="secondary">{item.reason}</Text></Space>} /></List.Item>} />
        </Card>
        <Card title="Recent learnings">
          <List size="small" dataSource={knowledge?.recent_learnings || []} locale={{ emptyText: 'No completed agent learnings yet' }} renderItem={item => { const learning = parseSummary(item.learning); return <List.Item><List.Item.Meta title={learning.user_request || learning.change_summary || learning.rejected_approach || item.run_id} description={<Space wrap>{(learning.files_inspected || learning.files_changed || []).slice(0, 4).map(path => <Tag key={path}>{path}</Tag>)}{item.patch_status && <Tag>{item.patch_status}</Tag>}</Space>} /></List.Item>; }} />
        </Card>
        <Card title="File summaries" className="knowledge-files">
          <List dataSource={knowledge?.file_summaries || []} locale={{ emptyText: 'No file summaries yet' }} renderItem={item => {
            const summary = parseSummary(item.summary);
            return <List.Item actions={[<Button type="link" key="open" onClick={() => onOpenFile(item.path)}>Open source</Button>, <Button type="link" key="reindex" onClick={() => reindexFile(item.path)}>Re-index</Button>]}><List.Item.Meta title={item.path} description={summary.purpose || item.summary} /></List.Item>;
          }} />
        </Card>
      </section>
      <Modal open={settingsOpen} title="Repository indexing settings" okText="Save settings" onOk={saveSettings} onCancel={() => setSettingsOpen(false)}>
        <Space direction="vertical" style={{ width: '100%' }} size="middle">
          <label><Text strong>Included paths</Text><Input.TextArea rows={3} placeholder="Leave empty for the complete workspace" value={(settings.include_paths || []).join('\n')} onChange={event => setSettings(current => ({ ...current, include_paths: lines(event.target.value) }))} /></label>
          <label><Text strong>Excluded directories or patterns</Text><Input.TextArea rows={4} value={(settings.exclude_paths || []).join('\n')} onChange={event => setSettings(current => ({ ...current, exclude_paths: lines(event.target.value) }))} /></label>
          <label><Text strong>Sensitive filename patterns</Text><Input.TextArea rows={4} value={(settings.sensitive_patterns || []).join('\n')} onChange={event => setSettings(current => ({ ...current, sensitive_patterns: lines(event.target.value) }))} /></label>
          <label><Text strong>Maximum file size in bytes</Text><br /><InputNumber min={1024} max={10485760} style={{ width: '100%' }} value={settings.maximum_file_size_bytes} onChange={value => setSettings(current => ({ ...current, maximum_file_size_bytes: value }))} /></label>
          <Alert type="warning" showIcon message="Secrets remain excluded by default. Re-index the workspace after changing these settings." />
        </Space>
      </Modal>
    </main>
  );
}
