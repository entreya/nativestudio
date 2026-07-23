import React, { useState } from 'react';
import { Typography } from 'antd';
import {
  BulbOutlined,
  SearchOutlined,
  FileSearchOutlined,
  FolderOpenOutlined,
  GlobalOutlined,
  EyeOutlined,
  EditOutlined,
  FileAddOutlined,
  DeleteOutlined,
  DiffOutlined,
  CodeOutlined,
  ToolOutlined,
  CheckCircleFilled,
  CloseCircleFilled,
  LoadingOutlined,
  DownOutlined,
  RightOutlined,
} from '@ant-design/icons';

const { Text } = Typography;

// One icon per tool so the timeline reads at a glance, the way Claude's own
// transcript distinguishes a file read from a search from an edit.
const TOOL_ICONS = {
  find_files: FileSearchOutlined,
  list_directory: FolderOpenOutlined,
  list_files: FolderOpenOutlined,
  search_text: SearchOutlined,
  search_internet: GlobalOutlined,
  read_file: EyeOutlined,
  read_file_range: EyeOutlined,
  get_editor_context: EyeOutlined,
  replace_in_file: EditOutlined,
  apply_patch: DiffOutlined,
  create_file: FileAddOutlined,
  delete_file: DeleteOutlined,
  run_command: CodeOutlined,
};

// Fields containing full file/diff bodies — never dumped as part of the
// clean key/value input list; PatchReview (or a dedicated preview below)
// already shows the real content.
const LARGE_CONTENT_FIELDS = new Set(['content', 'diff', 'old_text', 'new_text']);

const FIELD_LABELS = { query: 'Query', path: 'Path', extensions: 'Extensions', file_types: 'Type', limit: 'Limit', max_results: 'Max results', depth: 'Depth', command: 'Command', cwd: 'Directory' };

function iconFor(entry) {
  if (entry.status === 'error') return CloseCircleFilled;
  if (entry.status === 'running') return LoadingOutlined;
  if (entry.kind === 'thinking') return BulbOutlined;
  if (entry.kind === 'context') return SearchOutlined;
  return TOOL_ICONS[entry.name] || ToolOutlined;
}

function colorFor(entry) {
  if (entry.status === 'error') return '#b85c5c';
  if (entry.status === 'running') return 'var(--studio-accent, #c15f3c)';
  return '#6b8f71';
}

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
  // kind === 'tool'
  const { name, input, output, status, error } = entry;
  switch (name) {
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

function blockStyle(isError) {
  return {
    margin: '4px 0 0', padding: 8, borderRadius: 6, fontSize: 11.5, lineHeight: 1.5,
    overflowX: 'auto', whiteSpace: 'pre-wrap', maxHeight: 240, overflowY: 'auto',
    background: isError ? 'rgba(239, 68, 68, 0.08)' : 'var(--studio-panel, #f5f1e9)',
    color: isError ? '#991b1b' : 'inherit',
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

const KIND_BODY = { thinking: ThinkingBody, context: ContextBody, tool: ToolBody };

// ICON_COLUMN is the width reserved for the icon/connector column; the line
// (drawn once for the whole timeline, not per-row — see ThinkingTimeline)
// sits at its horizontal center.
const ICON_COLUMN = 18;

function TimelineRow({ entry, isFirst, isLast, expanded, onToggle }) {
  const Icon = iconFor(entry);
  const color = colorFor(entry);
  const Body = KIND_BODY[entry.kind] || ToolBody;
  return (
    <div style={{ display: 'grid', gridTemplateColumns: `${ICON_COLUMN}px 1fr`, gap: 8 }}>
      <div style={{ display: 'flex', justifyContent: 'center' }}>
        {/* A solid-background wrapper masks the single continuous connector
            line (drawn once behind the whole timeline) where it passes
            behind this icon, top and bottom. */}
        <span style={{ position: 'relative', zIndex: 1, marginTop: 2, padding: '3px 0', background: 'var(--studio-bg, #f7f4ed)' }}>
          {/* eslint-disable-next-line react-hooks/static-components -- Icon is
              picked from a fixed table of module-level icon components keyed
              by entry status/kind/name, not created fresh each render; the
              rule can't see through iconFor() to know that. */}
          <Icon spin={entry.status === 'running'} style={{ color, fontSize: 12, display: 'block' }} />
        </span>
      </div>
      <div style={{ minWidth: 0, paddingTop: isFirst ? 0 : 2, paddingBottom: isLast ? 0 : 11 }}>
        <button
          type="button"
          onClick={onToggle}
          style={{ display: 'flex', alignItems: 'center', gap: 5, width: '100%', textAlign: 'left', background: 'transparent', border: 0, padding: '2px 0', cursor: 'pointer' }}
        >
          {expanded ? <DownOutlined style={{ fontSize: 9, color: 'var(--studio-subtle, #91877e)', flexShrink: 0 }} /> : <RightOutlined style={{ fontSize: 9, color: 'var(--studio-subtle, #91877e)', flexShrink: 0 }} />}
          <Text style={{ color: 'var(--studio-muted, #746b63)', fontSize: 11.5, whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>
            {summaryFor(entry)}
          </Text>
        </button>
        {expanded && <div style={{ marginTop: 4 }}><Body entry={entry} /></div>}
      </div>
    </div>
  );
}

/**
 * A single chronological timeline of everything the agent did to produce a
 * response — thinking, context gathering, and tool calls in the order they
 * actually happened, each collapsible. This replaces a previous split
 * timeline/output-pane layout with one linear stream, closer to how Claude's
 * own transcript reads.
 *
 * Props:
 *  - entries: TimelineEntry[] — { id, kind: 'thinking'|'context'|'tool', status, ...kind-specific fields }
 *  - running: whether the agent is still actively producing this message
 */
export default function ThinkingTimeline({ entries = [], running = false, hasContent = false }) {
  const [overrides, setOverrides] = useState({});

  // Nothing has streamed in yet — still show an active row (icon, spinner,
  // connector) instead of a plain, disconnected "thinking" line, so there's
  // never a dead gap between hitting send and the first visible step. Once
  // real answer text has started streaming there's no gap left to fill.
  if (entries.length === 0) {
    if (!running || hasContent) return null;
    return (
      <div style={{ margin: '4px 0 12px' }} aria-label="Agent thinking timeline">
        <TimelineRow entry={{ id: 'working', kind: 'tool', name: 'working', status: 'running' }} isFirst isLast expanded={false} onToggle={() => {}} />
      </div>
    );
  }

  const isExpanded = (entry, isLast) => {
    if (entry.id in overrides) return overrides[entry.id];
    return running && isLast;
  };
  const toggle = (id, current) => setOverrides(prev => ({ ...prev, [id]: !current }));

  return (
    <div style={{ margin: '4px 0 12px', position: 'relative' }} aria-label="Agent thinking timeline">
      {/* One continuous connector line behind every icon, instead of a
          separate segment per row — guaranteed gap-free regardless of how
          tall an expanded entry's body renders. */}
      {entries.length > 1 && (
        <span style={{ position: 'absolute', left: (ICON_COLUMN - 1) / 2, top: 8, bottom: 8, width: 1, background: 'var(--studio-border, #d8d1c5)' }} />
      )}
      {entries.map((entry, index) => {
        const isFirst = index === 0;
        const isLast = index === entries.length - 1;
        const expanded = isExpanded(entry, isLast);
        return (
          <TimelineRow key={entry.id} entry={entry} isFirst={isFirst} isLast={isLast} expanded={expanded} onToggle={() => toggle(entry.id, expanded)} />
        );
      })}
    </div>
  );
}
