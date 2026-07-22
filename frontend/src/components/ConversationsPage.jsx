import React, { useEffect, useMemo, useState } from 'react';
import { Alert, Button, Empty, Input, Popconfirm, Progress, Spin, Tag, Typography } from 'antd';
import {
  ClockCircleOutlined,
  DatabaseOutlined,
  DeleteOutlined,
  FolderOpenOutlined,
  MessageOutlined,
  SearchOutlined,
} from '@ant-design/icons';

const { Text, Title, Paragraph } = Typography;

const folderName = (path) => path?.split(/[\\/]/).filter(Boolean).pop() || 'Unknown folder';
const number = new Intl.NumberFormat();

export default function ConversationsPage({ onOpenConversation }) {
  const [conversations, setConversations] = useState([]);
  const [loading, setLoading] = useState(true);
  const [query, setQuery] = useState('');
  const [error, setError] = useState('');
  const [reloadKey, setReloadKey] = useState(0);

  useEffect(() => {
    let cancelled = false;
    fetch('/api/sessions')
      .then(async res => {
        const contentType = res.headers.get('content-type') || '';
        if (!contentType.includes('application/json')) {
          throw new Error('The conversations API is unavailable. Restart the NativeStudio backend to load the new route.');
        }
        const data = await res.json();
        if (!res.ok) throw new Error(data.message || data.error || 'Could not load conversations');
        if (!cancelled) setConversations(data.conversations || []);
      })
      .catch(error => {
        console.error(error);
        if (!cancelled) {
          setConversations([]);
          setError(error.message || 'Could not load conversations.');
        }
      })
      .finally(() => { if (!cancelled) setLoading(false); });
    return () => { cancelled = true; };
  }, [reloadKey]);

  const visibleConversations = useMemo(() => {
    const needle = query.trim().toLowerCase();
    if (!needle) return conversations;
    return conversations.filter(item => [item.title, item.model, item.project_path, item.summary, item.latest_memory]
      .some(value => value?.toLowerCase().includes(needle)));
  }, [conversations, query]);

  const totals = useMemo(() => conversations.reduce((sum, item) => ({
    messages: sum.messages + item.message_count,
    tokens: sum.tokens + item.tokens_used,
    memories: sum.memories + item.checkpoint_count,
  }), { messages: 0, tokens: 0, memories: 0 }), [conversations]);

  const deleteConversation = async (id) => {
    const res = await fetch(`/api/sessions/${id}`, { method: 'DELETE' });
    if (res.ok) setConversations(items => items.filter(item => item.id !== id));
  };

  return (
    <main className="memory-page">
      <section className="memory-hero">
        <div>
          <Text className="eyebrow">YOUR WORKSPACE HISTORY</Text>
          <Title level={1}>Conversations & memory</Title>
          <Paragraph>Return to previous threads and see what NativeStudio has retained across your projects.</Paragraph>
        </div>
        <Input
          allowClear
          prefix={<SearchOutlined />}
          placeholder="Search conversations or memory"
          value={query}
          onChange={event => setQuery(event.target.value)}
          className="memory-search"
        />
      </section>

      <section className="memory-stats" aria-label="Memory summary">
        <div><MessageOutlined /><span><strong>{number.format(conversations.length)}</strong> conversations</span></div>
        <div><DatabaseOutlined /><span><strong>{number.format(totals.memories)}</strong> memory checkpoints</span></div>
        <div><ClockCircleOutlined /><span><strong>{number.format(totals.tokens)}</strong> remembered tokens</span></div>
      </section>

      {error ? (
        <div className="memory-empty">
          <Alert
            type="error"
            showIcon
            message="Conversations could not be loaded"
            description={error}
            action={<Button onClick={() => { setError(''); setLoading(true); setReloadKey(key => key + 1); }}>Retry</Button>}
          />
        </div>
      ) : loading ? (
        <div className="memory-loading"><Spin size="large" /></div>
      ) : visibleConversations.length === 0 ? (
        <div className="memory-empty"><Empty description={query ? 'No conversations match your search' : 'No conversations yet'} /></div>
      ) : (
        <section className="conversation-grid">
          {visibleConversations.map(item => {
            const usage = item.token_budget > 0 ? Math.min(100, Math.round((item.tokens_used / item.token_budget) * 100)) : 0;
            return (
              <article className="conversation-card" key={item.id}>
                <div className="conversation-card-top">
                  <div className="conversation-icon"><MessageOutlined /></div>
                  <div className="conversation-heading">
                    <Title level={4}>{item.title || 'Untitled conversation'}</Title>
                    <Text><FolderOpenOutlined /> {folderName(item.project_path)}</Text>
                  </div>
                  <Popconfirm
                    title="Delete this conversation?"
                    description="Messages and memory checkpoints will be removed."
                    okText="Delete"
                    okButtonProps={{ danger: true }}
                    onConfirm={() => deleteConversation(item.id)}
                  >
                    <Button type="text" danger aria-label="Delete conversation" icon={<DeleteOutlined />} />
                  </Popconfirm>
                </div>

                <div className="conversation-meta">
                  <Tag>{item.model}</Tag>
                  <span>{item.message_count} messages</span>
                  <span>{new Date(item.updated_at).toLocaleString()}</span>
                </div>

                <div className="memory-block">
                  <div className="memory-block-title">
                    <span><DatabaseOutlined /> Memory</span>
                    <span>{item.checkpoint_count} checkpoints</span>
                  </div>
                  <Paragraph ellipsis={{ rows: 3 }}>
                    {item.summary || item.latest_memory || 'No AI summary yet. It will be created after the next completed exchange.'}
                  </Paragraph>
                  <div className="memory-usage">
                    <Progress percent={usage} showInfo={false} size="small" strokeColor="var(--studio-accent, #c15f3c)" trailColor="var(--studio-border, #ded7ca)" />
                    <Text>{number.format(item.tokens_used)} / {number.format(item.token_budget)} tokens</Text>
                  </div>
                </div>

                <Button className="open-conversation" onClick={() => onOpenConversation(item)}>
                  Open conversation
                </Button>
              </article>
            );
          })}
        </section>
      )}
    </main>
  );
}
