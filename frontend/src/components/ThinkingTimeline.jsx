import React from 'react';
import { Typography } from 'antd';
import { DownOutlined } from '@ant-design/icons';

const { Text } = Typography;

// Fields containing full file/diff bodies — never dumped as part of the
// clean key/value input list; PatchReview (or a dedicated preview below)
// already shows the real content.
const LARGE_CONTENT_FIELDS = new Set(['content', 'diff', 'old_text', 'new_text']);

const FIELD_LABELS = { query: 'Query', path: 'Path', extensions: 'Extensions', file_types: 'Type', limit: 'Limit', max_results: 'Max results', depth: 'Depth', command: 'Command', cwd: 'Directory' };

/** One-line summary shown collapsed — the thing a user scans to follow along. */
function summaryFor(entry) {
  if (entry.kind === 'thinking') {
    return entry.status === 'running' ? 'Thinking…' : 'Thought';
  }
  if (entry.kind === 'context') {
    const count = entry.count || 0;
    if (entry.status === 'running') return 'Gathering context…';
    if (count === 0) return 'Checked project context';
    return `Gathered context from ${count} source${count === 1 ? '' : 's'}`;
  }
  if (entry.kind === 'step') {
    // Covers the otherwise-silent stretch while Ollama loads the model and
    // evaluates the prompt, before the first token arrives.
    return entry.status === 'running' ? (entry.text || 'Working…') : (entry.doneText || 'Planned next action');
  }
  if (entry.kind === 'rephrase') {
    return 'Clarified your request';
  }
  // kind === 'tool'
  const { name, input, output, status, error } = entry;
  switch (name) {
    case 'working':
      return 'Working…';
    case 'find_files': {
      const q = input?.query ? `"${input.query}"` : '';
      if (status !== 'done') return `Searching files for ${q}`;
      if (status === 'error') return `Search failed for ${q}`;
      const total = output?.total_matches ?? output?.matches?.length ?? 0;
      return `Found ${total} matching file${total === 1 ? '' : 's'}${output?.truncated ? ' (truncated)' : ''}`;
    }
    case 'list_directory':
      if (status !== 'done') return `Listing ${input?.path || 'workspace root'}`;
      if (status === 'error') return `Could not list ${input?.path || 'directory'}`;
      return `Listed ${output?.entries?.length ?? 0} item${(output?.entries?.length ?? 0) === 1 ? '' : 's'} in ${output?.path || input?.path || 'workspace root'}`;
    case 'search_internet':
      if (status === 'error') return `Search failed: ${input?.query || 'the internet'}`;
      return status === 'done' ? `Searched: ${input?.query || 'the internet'}` : `Searching: ${input?.query || 'the internet'}`;
    case 'search_text':
      return status === 'done' ? `Searched for "${input?.query || ''}"` : `Searching for "${input?.query || ''}"`;
    case 'read_file':
    case 'read_file_range':
      return `Read ${input?.path || 'a file'}`;
    case 'replace_in_file':
    case 'apply_patch':
      return `Proposed an edit to ${input?.path || 'a file'}`;
    case 'create_file':
      return `Proposed creating ${input?.path || 'a file'}`;
    case 'delete_file':
      return `Proposed deleting ${input?.path || 'a file'}`;
    case 'get_editor_context':
      return 'Checked the editor context';
    case 'run_command':
      if (status === 'running') return `Staging command: ${input?.command || ''}`;
      if (status === 'error') return `Could not stage command: ${input?.command || ''}`;
      return `Waiting for your approval: ${input?.command || ''}`;
    default:
      if (status === 'error') return `${name} failed${error ? `: ${error}` : ''}`;
      return status === 'done' ? `Used ${name}` : `Using ${name}`;
  }
}

function ThinkingBody({ entry }) {
  return (
    <div style={{
      padding: 10, borderRadius: 6, fontSize: 12.5, lineHeight: 1.5, fontStyle: 'italic',
      color: 'var(--studio-muted, #746b63)', whiteSpace: 'pre-wrap',
      backgroundColor: 'var(--studio-panel, #fafaf0)', border: '1px solid var(--studio-border, #e8e8d0)',
      maxHeight: 260, overflowY: 'auto',
    }}>
      {entry.text || ''}
    </div>
  );
}

function ContextBody({ entry }) {
  const items = entry.items || [];
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
      {items.length === 0 ? (
        <Text type="secondary" style={{ fontSize: 11.5 }}>No related files were found.</Text>
      ) : items.map((item, index) => (
        <Text key={index} style={{ fontSize: 11.5, fontFamily: 'monospace', color: 'var(--studio-text, #2f2a26)' }}>
          {item.label}
        </Text>
      ))}
      {entry.tokens != null && (
        <Text type="secondary" style={{ fontSize: 11, marginTop: 4 }}>
          Context built · {entry.tokens} / {entry.budget || 0} tokens
        </Text>
      )}
    </div>
  );
}

function RephraseBody({ entry }) {
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
      <div>
        <Text type="secondary" style={{ fontSize: 11 }}>You typed</Text>
        <Text style={{ display: 'block', fontSize: 12, color: 'var(--studio-text, #2f2a26)' }}>{entry.original}</Text>
      </div>
      <div>
        <Text type="secondary" style={{ fontSize: 11 }}>Sent to the model as</Text>
        <Text style={{ display: 'block', fontSize: 12, fontStyle: 'italic', color: 'var(--studio-text, #2f2a26)' }}>{entry.rephrased}</Text>
      </div>
    </div>
  );
}

function blockStyle(isError) {
  return {
    margin: '4px 0 0', padding: 8, borderRadius: 6, fontSize: 11.5, lineHeight: 1.5,
    overflowX: 'auto', whiteSpace: 'pre-wrap', maxHeight: 240, overflowY: 'auto',
    background: isError ? 'color-mix(in srgb, var(--studio-danger, #b85c5c) 10%, transparent)' : 'var(--studio-panel, #f5f1e9)',
    color: isError ? 'var(--studio-danger, #b85c5c)' : 'inherit',
  };
}

function humanValue(value) {
  if (Array.isArray(value)) return value.join(', ');
  if (typeof value === 'object' && value !== null) return JSON.stringify(value);
  return String(value);
}

/** Plain "Label: value" lines instead of a raw JSON dump — every tool gets
 * this basic legible form; specific tools additionally render a richer body
 * below (a match list, a directory listing, search results, ...). */
function InputSummary({ input }) {
  if (!input) return null;
  const fields = Object.entries(input).filter(([key, value]) => !LARGE_CONTENT_FIELDS.has(key) && value !== '' && value !== undefined && value !== null);
  if (fields.length === 0) return null;
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
      {fields.map(([key, value]) => (
        <Text key={key} style={{ fontSize: 11.5 }}>
          <Text type="secondary" style={{ fontSize: 11.5 }}>{FIELD_LABELS[key] || key}: </Text>
          {humanValue(value)}
        </Text>
      ))}
    </div>
  );
}

function ToolBody({ entry }) {
  const { name, input, output, error, status } = entry;

  // File-mutation tools: the real diff/content already renders in the patch
  // review card below once staged — the timeline just needs to say so.
  if (['create_file', 'replace_in_file', 'apply_patch', 'delete_file'].includes(name)) {
    return (
      <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
        <InputSummary input={input} />
        {error && <pre style={blockStyle(true)}>{error}</pre>}
        {output?.staged && <Text type="secondary" style={{ fontSize: 11 }}>Staged for review below — nothing is written until you approve it.</Text>}
      </div>
    );
  }

  if (name === 'run_command') {
    return (
      <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
        <pre style={blockStyle(false)}>{input?.command || ''}</pre>
        {input?.cwd && <Text type="secondary" style={{ fontSize: 11 }}>in {input.cwd}</Text>}
        {error && <pre style={blockStyle(true)}>{error}</pre>}
        {output?.staged && <Text type="secondary" style={{ fontSize: 11, fontStyle: 'italic' }}>Review and approve it below to actually run it — nothing has executed yet.</Text>}
      </div>
    );
  }

  if (name === 'find_files' && output?.matches) {
    return (
      <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
        <InputSummary input={input} />
        {output.matches.length === 0 ? (
          <Text type="secondary" style={{ fontSize: 11.5 }}>No matching files.</Text>
        ) : (
          <div style={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
            {output.matches.slice(0, 20).map((m, i) => (
              <Text key={i} style={{ fontSize: 11.5, fontFamily: 'monospace' }}>{m.path}</Text>
            ))}
          </div>
        )}
      </div>
    );
  }

  if (name === 'list_directory' && output?.entries) {
    return (
      <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
        <InputSummary input={input} />
        <div style={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
          {output.entries.slice(0, 30).map((e, i) => (
            <Text key={i} style={{ fontSize: 11.5, fontFamily: 'monospace' }}>{e.type === 'directory' ? `${e.name}/` : e.name}</Text>
          ))}
        </div>
      </div>
    );
  }

  if (name === 'search_internet' && output?.results) {
    return (
      <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
        {output.results.length === 0 ? (
          <Text type="secondary" style={{ fontSize: 11.5 }}>No results.</Text>
        ) : output.results.slice(0, 8).map((r, i) => (
          <Text key={i} style={{ fontSize: 11.5 }}>{r.title} — <Text type="secondary" style={{ fontSize: 11 }}>{r.url}</Text></Text>
        ))}
      </div>
    );
  }

  if ((name === 'read_file' || name === 'read_file_range') && output?.content) {
    const preview = output.content.length > 800 ? `${output.content.slice(0, 800)}…` : output.content;
    return (
      <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
        <InputSummary input={input} />
        <pre style={blockStyle(false)}>{preview}</pre>
      </div>
    );
  }

  // Generic fallback for anything else.
  const hasOutput = (output !== undefined && output !== '') || error;
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
      <InputSummary input={input} />
      {hasOutput && (
        <pre style={blockStyle(Boolean(error))}>
          {error || (typeof output === 'string' ? output : JSON.stringify(output, null, 2))}
        </pre>
      )}
      {status === 'running' && !hasOutput && (
        <Text type="secondary" style={{ fontSize: 11, fontStyle: 'italic' }}>Running…</Text>
      )}
    </div>
  );
}

const KIND_BODY = { thinking: ThinkingBody, context: ContextBody, tool: ToolBody, rephrase: RephraseBody };

/**
 * The collapsible right-hand content for one timeline entry — summary line,
 * expand chevron, and the expanded body. Deliberately has no icon or rail of
 * its own: ChatPanel renders every step as a row in the SAME flex-column
 * gutter it uses for the message's own dot, so a step's marker is just
 * another entry in that one list rather than a second rail that has to be
 * measured and offset to line up with the first.
 */
export function TimelineEntryContent({ entry, expanded, onToggle }) {
  const Body = KIND_BODY[entry.kind] || ToolBody;
  return (
    <div>
      <button
        type="button"
        onClick={onToggle}
        style={{
          display: 'flex', alignItems: 'center', gap: 6, width: '100%',
          background: expanded ? 'var(--studio-panel, #f1ece4)' : 'transparent',
          border: 'none', borderRadius: 6, padding: '3px 8px', margin: '-3px 0 0 -8px',
          cursor: 'pointer', color: 'var(--studio-muted, #746b63)', transition: 'background 0.15s',
          textAlign: 'left',
        }}
        onMouseEnter={e => { e.currentTarget.style.background = 'var(--studio-panel, #f1ece4)'; }}
        onMouseLeave={e => { if (!expanded) e.currentTarget.style.background = 'transparent'; }}
      >
        <Text ellipsis style={{ flex: 1, minWidth: 0, color: 'var(--studio-text, #2f2a26)', fontSize: 12, fontWeight: 500 }}>
          {summaryFor(entry)}
        </Text>
        <DownOutlined style={{
          flexShrink: 0, fontSize: 8, color: 'var(--studio-subtle, #91877e)',
          transform: expanded ? 'rotate(0deg)' : 'rotate(-90deg)', transition: 'transform 0.15s ease',
        }} />
      </button>
      {expanded && (
        <div style={{ marginTop: 6 }}>
          <Body entry={entry} />
        </div>
      )}
    </div>
  );
}
