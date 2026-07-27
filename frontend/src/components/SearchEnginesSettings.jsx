import React, { useEffect, useState } from 'react';
import { Typography, Switch, Button, Input, Tag, Space, Alert, Card, List, Popconfirm } from 'antd';
import { PlusOutlined, DeleteOutlined, ExperimentOutlined, GlobalOutlined } from '@ant-design/icons';

const { Text, Title, Paragraph } = Typography;

// Lets the user see which search engines the agent's search_internet /
// verified-fact-checking pipeline actually queries, turn any of them off,
// and add a new one — through an AI-assisted probe rather than by hand-
// writing a URL template and field mapping. A candidate is only ever saved
// after the probe proves it actually returns real results; nothing is
// trusted on the strength of a plausible-looking guess alone.
export default function SearchEnginesSettings() {
  const [engines, setEngines] = useState(null); // merged view (builtin + custom) for display
  const [rawOverrides, setRawOverrides] = useState([]); // what's actually stored under settings.searchEngines
  const [saving, setSaving] = useState(false);

  const [name, setName] = useState('');
  const [sampleUrl, setSampleUrl] = useState('');
  const [sampleQuery, setSampleQuery] = useState('');
  const [probing, setProbing] = useState(false);
  const [probeResult, setProbeResult] = useState(null);
  const [probeError, setProbeError] = useState('');

  const loadEngines = () => {
    fetch('/api/search-engines').then(res => res.json()).then(setEngines).catch(() => setEngines([]));
    fetch('/api/settings').then(res => res.json()).then(data => setRawOverrides(data.searchEngines || []));
  };

  useEffect(() => { loadEngines(); }, []);

  const persistOverrides = (nextOverrides) => {
    setSaving(true);
    fetch('/api/settings')
      .then(res => res.json())
      .then(current => fetch('/api/settings', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ ...current, searchEngines: nextOverrides }),
      }))
      .then(() => { setRawOverrides(nextOverrides); loadEngines(); })
      .finally(() => setSaving(false));
  };

  const toggleEngine = (engine, enabled) => {
    const others = rawOverrides.filter(o => o.id !== engine.id);
    if (engine.builtin) {
      persistOverrides([...others, { id: engine.id, enabled, name: engine.name }]);
    } else {
      persistOverrides([...others, { ...engine, enabled }]);
    }
  };

  const removeCustomEngine = (engine) => {
    persistOverrides(rawOverrides.filter(o => o.id !== engine.id));
  };

  const runProbe = () => {
    setProbing(true);
    setProbeError('');
    setProbeResult(null);
    fetch('/api/search-engines/probe', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name: name.trim(), sample_url: sampleUrl.trim(), sample_query: sampleQuery.trim() }),
    })
      .then(res => res.json())
      .then(result => {
        if (!result.ok) { setProbeError(result.message); return; }
        setProbeResult(result);
      })
      .catch(() => setProbeError('Could not reach the server to test this engine.'))
      .finally(() => setProbing(false));
  };

  const confirmAddEngine = () => {
    if (!probeResult?.engine) return;
    persistOverrides([...rawOverrides.filter(o => o.id !== probeResult.engine.id), probeResult.engine]);
    setName(''); setSampleUrl(''); setSampleQuery('');
    setProbeResult(null); setProbeError('');
  };

  const canProbe = name.trim() && sampleUrl.trim() && sampleQuery.trim() && !probing;

  return (
    <div style={{ marginTop: 24, display: 'flex', flexDirection: 'column', gap: 20 }}>
      <div>
        <Title level={4} style={{ color: 'var(--studio-text)', marginBottom: 8 }}>
          <GlobalOutlined style={{ marginRight: 8 }} /> Search Engines
        </Title>
        <Text style={{ color: 'var(--studio-muted)', display: 'block' }}>
          These are the engines search_internet queries in parallel when the agent needs current information — confidence is scored by how many of them independently agree, so turning more relevant ones on (or off, if one is noisy) directly affects how much the agent trusts what it finds.
        </Text>
      </div>

      <List
        bordered
        dataSource={engines || []}
        loading={engines === null}
        renderItem={engine => (
          <List.Item
            actions={[
              !engine.builtin && (
                <Popconfirm key="remove" title={`Remove ${engine.name}?`} onConfirm={() => removeCustomEngine(engine)}>
                  <Button size="small" type="text" danger icon={<DeleteOutlined />} />
                </Popconfirm>
              ),
              <Switch key="toggle" size="small" checked={engine.enabled} disabled={saving}
                onChange={checked => toggleEngine(engine, checked)} />,
            ].filter(Boolean)}
          >
            <List.Item.Meta
              title={<span>{engine.name} {engine.builtin && <Tag style={{ marginInlineStart: 6 }}>builtin</Tag>}</span>}
              description={<span>{engine.kind === 'json' ? 'JSON API' : 'RSS feed'} · {engine.category}{engine.derives_from ? ` · derives from ${engine.derives_from}` : ''}</span>}
            />
          </List.Item>
        )}
      />

      <Card size="small" title={<span><PlusOutlined style={{ marginRight: 6 }} />Add a search engine</span>}>
        <Paragraph style={{ color: 'var(--studio-muted)', fontSize: 12, marginBottom: 12 }}>
          Search that engine for something in your browser, then paste the results page URL and the exact text you searched for — NativeStudio finds your query inside the URL to work out how to search it again, fetches it for real, and only offers to add it once that's confirmed to actually return results.
        </Paragraph>
        <Space direction="vertical" style={{ width: '100%' }}>
          <Input placeholder="Name (e.g. Marginalia Search)" value={name} onChange={e => setName(e.target.value)} />
          <Input placeholder="Results page URL you got after searching" value={sampleUrl} onChange={e => setSampleUrl(e.target.value)} />
          <Input placeholder="Exactly what you searched for" value={sampleQuery} onChange={e => setSampleQuery(e.target.value)} />
          <Button icon={<ExperimentOutlined />} loading={probing} disabled={!canProbe} onClick={runProbe}>
            Test this engine
          </Button>
        </Space>

        {probeError && <Alert style={{ marginTop: 12 }} type="error" showIcon message={probeError} />}

        {probeResult && (
          <Alert
            style={{ marginTop: 12 }}
            type="success"
            showIcon
            message={probeResult.message}
            description={
              <div>
                <ul style={{ paddingInlineStart: 18, margin: '8px 0' }}>
                  {(probeResult.sample || []).slice(0, 3).map((s, i) => (
                    <li key={i}><Text strong>{s.title}</Text>{s.description ? <Text type="secondary"> — {s.description.slice(0, 80)}</Text> : null}</li>
                  ))}
                </ul>
                <Button type="primary" size="small" onClick={confirmAddEngine} loading={saving}>Add this engine</Button>
              </div>
            }
          />
        )}
      </Card>
    </div>
  );
}
