/* eslint-disable react-hooks/exhaustive-deps -- loaders intentionally follow project/session identity changes, not function recreation */
import React, { useState, useEffect, useRef } from 'react';
import { Select, Progress, Checkbox, Radio, Button, Typography, Input, ConfigProvider, Switch, Tag, Popconfirm, BorderBeam, Tooltip } from 'antd';
import {
  SendOutlined,
  PlusOutlined,
  DownOutlined,
  StarFilled,
  MessageOutlined,
  BulbOutlined,
  CodeOutlined,
  SafetyCertificateOutlined,
  FileOutlined,
  DeleteOutlined,
  CopyOutlined,
  EditOutlined,
  RedoOutlined,
  BorderOutlined,
  CheckOutlined,
  CloseOutlined,
  ArrowUpOutlined,
} from '@ant-design/icons';
import { TimelineEntryContent } from './ThinkingTimeline';
import { iconFor, colorFor, workingPlaceholderEntry } from './timelineEntry';
import CodeSnippet from './CodeSnippet';
import PatchReview from './PatchReview';
import CommandReview from './CommandReview';
import AppliedPatchReview from './AppliedPatchReview';

const { Text } = Typography;
const { TextArea } = Input;

// Small local models sometimes disobey the "never print a tool-call JSON
// object" instruction and narrate the call (or its result) as a fenced code
// block instead of actually invoking the tool. That JSON is pure noise —
// the real, correctly-executed call already has its own row in the timeline
// above — so it's dropped rather than rendered as if it were code the user
// asked for.
function isLeakedToolJSON(value) {
  let parsed;
  try {
    parsed = JSON.parse(value);
  } catch {
    return false;
  }
  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) return false;
  const looksLikeCall = typeof parsed.name === 'string' && parsed.arguments && typeof parsed.arguments === 'object';
  const looksLikeResult = typeof parsed.ok === 'boolean' && ('staged' in parsed || 'patch_id' in parsed || 'run_id' in parsed || 'operation' in parsed);
  return looksLikeCall || looksLikeResult;
}

// The model sometimes writes the leaked JSON bare — not even fenced — as its
// own line of "prose". Strip any standalone line that's a complete JSON
// object matching the leaked-call/result shape, leaving the rest of the
// surrounding text intact.
function stripLeakedInlineJSON(text) {
  return text
    .split('\n')
    .filter(line => !isLeakedToolJSON(line.trim()))
    .join('\n');
}

function MessageContent({ content }) {
  const parts = [];
  const pattern = /```([\w-]*)\r?\n([\s\S]*?)```/g;
  let cursor = 0;
  let match;
  while ((match = pattern.exec(content || '')) !== null) {
    if (match.index > cursor) parts.push({ type: 'text', value: content.slice(cursor, match.index) });
    parts.push({ type: 'code', language: match[1] || 'text', value: match[2].replace(/\n$/, '') });
    cursor = pattern.lastIndex;
  }
  if (cursor < (content || '').length) parts.push({ type: 'text', value: content.slice(cursor) });

  return parts.map((part, index) => {
    if (part.type !== 'code') return <div key={index} style={{ whiteSpace: 'pre-wrap' }}>{stripLeakedInlineJSON(part.value)}</div>;
    if (isLeakedToolJSON(part.value.trim())) return null;
    return <CodeSnippet key={index} language={part.language} code={part.value} />;
  });
}

function sameWorkspacePath(left, right) {
  const normalize = value => (value || '').replaceAll('\\', '/').replace(/^\.\//, '').replace(/^\//, '');
  return normalize(left) === normalize(right);
}

// ── Timeline reducer helpers ────────────────────────────────────────────────
// The agent's run is rendered as one chronological list of entries (context
// gathering, thinking, tool calls) instead of separate thinking/tool-call
// state, so the UI reads as a single linear trace the way Claude's own
// transcript does.

function closeRunningEntries(timeline) {
  return timeline.map(entry => entry.status === 'running' ? { ...entry, status: 'done' } : entry);
}

function withContextEntry(timeline, apply) {
  const index = timeline.findIndex(entry => entry.kind === 'context');
  if (index === -1) {
    return [...timeline, apply({ id: 'context', kind: 'context', status: 'running', items: [], count: 0 })];
  }
  const next = [...timeline];
  next[index] = apply(next[index]);
  return next;
}

// Rebuilds the same `timeline` entry array the live SSE handler below builds
// incrementally, but from the flat event log the backend persists with the
// message (agent.go's persistedTimelineEvents). Used when loading a past
// conversation, so a refresh shows the same reasoning trace the user watched
// live instead of a bare answer with no steps shown — only the six event
// types the backend actually stores are handled here; unlisted ones (token,
// usage, agent_done, …) never appear in the stored log.
function replayTimeline(events) {
  let timeline = [];
  for (const { type, data } of events || []) {
    switch (type) {
      case 'agent_step': {
        const last = timeline[timeline.length - 1];
        if (last && last.kind === 'step' && last.status === 'running') {
          timeline = [...timeline.slice(0, -1), { ...last, text: data.message || last.text }];
        } else {
          timeline = [...closeRunningEntries(timeline), {
            id: `step-${data.step ?? timeline.length}`, kind: 'step', status: 'running', text: data.message || 'Working…',
          }];
        }
        break;
      }
      case 'thinking_token': {
        const last = timeline[timeline.length - 1];
        if (last && last.kind === 'thinking' && last.status === 'running') {
          timeline = [...timeline.slice(0, -1), { ...last, text: (last.text || '') + (data.text || '') }];
        } else {
          timeline = [...timeline, { id: `thinking-${timeline.length}`, kind: 'thinking', status: 'running', text: data.text }];
        }
        break;
      }
      case 'tool_call':
        timeline = [...closeRunningEntries(timeline), { id: data.id, kind: 'tool', status: 'running', name: data.name, input: data.input }];
        break;
      case 'tool_result':
        timeline = timeline.map(entry => entry.id === data.id
          ? { ...entry, status: data.ok ? 'done' : 'error', output: data.output, error: data.error }
          : entry);
        break;
      case 'context_resolved':
        timeline = withContextEntry(timeline, entry => ({
          ...entry,
          items: [...entry.items, { label: `${data.file}${data.symbol ? ` › ${data.symbol.name}` : ''} (${data.confidence})` }],
          count: entry.count + 1,
        }));
        break;
      case 'context_candidate':
        timeline = withContextEntry(timeline, entry => ({
          ...entry,
          items: [...entry.items, { label: `${data.path || data.kind}${data.symbol ? ` › ${data.symbol}` : ''}` }],
          count: entry.count + 1,
        }));
        break;
      case 'context_built':
        timeline = withContextEntry(timeline, entry => ({ ...entry, status: 'done', tokens: data.tokens, budget: data.budget }));
        break;
      case 'prompt_rephrased':
        timeline = [...closeRunningEntries(timeline), {
          id: `rephrase-${timeline.length}`, kind: 'rephrase', status: 'done',
          original: data.original, rephrased: data.rephrased,
        }];
        break;
      default:
        break;
    }
  }
  // A restored conversation is always finished — nothing should still read "running".
  return closeRunningEntries(timeline);
}

// Messages come back from /api/sessions/:id/messages with `timeline` as the
// raw JSON string the backend stored (or absent/empty). Parse it into the
// entry array the renderer expects; anything malformed or missing degrades
// to no timeline rather than breaking the message list.
function hydrateMessage(msg) {
  if (!msg || msg.role !== 'assistant' || !msg.timeline) return msg;
  try {
    const events = JSON.parse(msg.timeline);
    return { ...msg, timeline: replayTimeline(events) };
  } catch {
    return { ...msg, timeline: [] };
  }
}

// One row in the single conversation-wide rail: a message's own role dot, or
// one of its agent's reasoning steps below it — both rendered through this
// same component. The gutter uses plain flexbox centering (icon + a flex:1
// connector filling whatever space is left) instead of measuring one rail's
// dot size against another's, so a step's marker naturally lines up under
// the message dot above it with no coordinates to keep in sync, and the
// connector naturally stretches to match however tall the row's content
// turns out to be.
function Row({ dot, drawConnector, spacious, children }) {
  return (
    <div style={{ display: 'flex', gap: 16 }}>
      <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', width: 20, flexShrink: 0 }}>
        {dot}
        {drawConnector && <div style={{ flex: 1, width: 2, minHeight: 6, backgroundColor: 'var(--studio-border, #e2dcd4)', opacity: 0.4, marginTop: 2 }} />}
      </div>
      <div style={{ flex: 1, minWidth: 0, paddingBottom: spacious ? 16 : 10 }}>{children}</div>
    </div>
  );
}

function RoleDot({ color, pulsing }) {
  return (
    <div style={{
      width: 10, height: 10, borderRadius: '50%', flexShrink: 0, marginTop: 4,
      backgroundColor: color, animation: pulsing ? 'ai-pulse 1.5s infinite ease-in-out' : 'none',
    }} />
  );
}

function StepDot({ entry }) {
  const color = colorFor(entry);
  const Icon = iconFor(entry);
  return (
    <div style={{
      width: 18, height: 18, borderRadius: '50%', flexShrink: 0, marginTop: 1,
      display: 'flex', alignItems: 'center', justifyContent: 'center',
      backgroundColor: 'var(--studio-bg, #f7f4ed)', border: `1.5px solid ${color}`,
    }}>
      {/* eslint-disable-next-line react-hooks/static-components -- Icon is
          picked from a fixed table of module-level icon components keyed by
          entry status/kind/name, not created fresh each render; the rule
          can't see through iconFor() to know that. */}
      <Icon spin={entry.status === 'running'} style={{ color, fontSize: 8.5 }} />
    </div>
  );
}

export default function ChatPanel({
  activeFilePath,
  activeProject,
  editorContext,
  requestedSessionId,
  onFilesChanged,
  onTerminalActivity,
}) {
  // models: plain name strings shown in the dropdown.
  // modelCapMap: name → thinking_capable flag, populated from /api/models.
  // Keeping them separate avoids spreading objects into Ant Design's <Select>.
  const [models, setModels] = useState([]);
  const modelCapMapRef = useRef({});
  const [currentModel, setCurrentModel] = useState('');
  const [isThinkingModel, setIsThinkingModel] = useState(false);
  const [thinkMode, setThinkMode] = useState(false);
  // No UI control sets this yet — always sent as 'medium'. Kept as state
  // rather than a plain constant so a future thinking-level picker only
  // needs to add the setter call, not restructure this.
  const [thinkLevel] = useState('medium');

  const [sessions, setSessions] = useState([]);
  const [loadedProjectId, setLoadedProjectId] = useState('');
  const [activeSession, setActiveSession] = useState(null);
  const [sessionsOpen, setSessionsOpen] = useState(false);

  const [contextData, setContextData] = useState({ pct: 0, zone: 'green', tokensUsed: 0, tokensTotal: 0 });
  const [liveUsage, setLiveUsage] = useState({ prompt: 0, completion: 0 });
  const [messages, setMessages] = useState([]);
  const [input, setInput] = useState('');
  const [followUpAnswers, setFollowUpAnswers] = useState({});
  // Expand/collapse state for individual timeline steps, keyed by
  // `${messageIndex}:${entryId}` since entry ids (e.g. 'context') repeat
  // across different messages' own timelines.
  const [timelineOverrides, setTimelineOverrides] = useState({});
  const [editingMessage, setEditingMessage] = useState(null);
  const [editDraft, setEditDraft] = useState('');
  const [pendingPatches, setPendingPatches] = useState([]);
  const [appliedPatches, setAppliedPatches] = useState([]);
  const [pendingCommands, setPendingCommands] = useState([]);

  const [agentRunning, setAgentRunning] = useState(false);
  const [includeFile, setIncludeFile] = useState(true);
  const [promptFocused, setPromptFocused] = useState(false);
  const [followUpFocusedIndex, setFollowUpFocusedIndex] = useState(null);

  const messagesEndRef = useRef(null);
  const inputRef = useRef(null);
  const abortControllerRef = useRef(null);
  const streamStateRef = useRef({ assistantText: '', completionChars: 0, thinkingChars: 0 });

  // Look up thinking capability from the server-provided map.
  // The backend (agent.SupportsNativeThinking) is the single source of truth.
  const supportsThinking = (modelName) => modelCapMapRef.current[modelName] ?? false;

  useEffect(() => {
    return () => abortControllerRef.current?.abort();
  }, []);

  useEffect(() => {
    fetch('/api/models')
      .then(res => res.json())
      .then(data => {
        if (data.models && data.models.length > 0) {
          // API returns [{name, thinking_capable}, ...] — build a lookup map
          // and extract plain name strings for the dropdown.
          const capMap = {};
          const names = data.models.map(m => {
            const name = typeof m === 'string' ? m : m.name;
            capMap[name] = typeof m === 'object' ? Boolean(m.thinking_capable) : false;
            return name;
          });
          modelCapMapRef.current = capMap;
          setModels(names);
          const defaultModel = names.includes(data.default_model) ? data.default_model : names[0];
          setCurrentModel(defaultModel);
          setIsThinkingModel(capMap[defaultModel] ?? false);
        }
      });
  }, []);

  const handleModelChange = (m) => {
    // m is a plain model name string (Ant Design Select passes value, not the option object)
    const supported = modelCapMapRef.current[m] ?? false;
    setCurrentModel(m);
    setIsThinkingModel(supported);
    if (!supported) setThinkMode(false);
  };

  useEffect(() => {
    if (activeProject) loadSessions();
  }, [activeProject]);

  async function loadSessions() {
    try {
      const res = await fetch(`/api/projects/${activeProject.id}/sessions`);
      const data = await res.json();
      setSessions(data.sessions || []);
      if (data.sessions && data.sessions.length > 0) {
        const requested = data.sessions.find(session => session.id === requestedSessionId);
        selectSession(requested || data.sessions[0]);
      }
    } catch (e) {
      console.error(e);
    } finally {
      setLoadedProjectId(activeProject.id);
    }
  }

  useEffect(() => {
    if (loadedProjectId === activeProject?.id && activeProject && sessions.length === 0 && currentModel && !activeSession) {
      handleNewSession();
    }
  }, [loadedProjectId, currentModel, activeProject, sessions.length, activeSession]);

  async function handleNewSession() {
    try {
      const res = await fetch(`/api/projects/${activeProject.id}/sessions`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ title: 'New Conversation', model: currentModel || 'qwen2.5-coder:1.5b' })
      });
      const data = await res.json();
      if (res.ok) {
        setSessions(prev => [data.session, ...prev]);
        selectSession(data.session);
        setSessionsOpen(false);
      }
    } catch (e) {
      console.error(e);
    }
  }

  const handleDeleteSession = async (session) => {
    if (!session || agentRunning) return;
    try {
      const res = await fetch(`/api/sessions/${session.id}`, { method: 'DELETE' });
      if (!res.ok) throw new Error('Failed to delete conversation');
      const remaining = sessions.filter(s => s.id !== session.id);
      setSessions(remaining);
      if (activeSession?.id === session.id) {
        setActiveSession(null);
        setMessages([]);
        if (remaining.length > 0) await selectSession(remaining[0]);
      }
    } catch (e) {
      console.error(e);
      window.alert('Could not delete this conversation.');
    }
  };

  async function selectSession(sess) {
    const supported = supportsThinking(sess.model);
    setActiveSession(sess);
    setCurrentModel(sess.model);
    setIsThinkingModel(supported);
    if (!supported) setThinkMode(false);
    setSessionsOpen(false);

    try {
      const [messagesResponse, patchesResponse, commandsResponse] = await Promise.all([
        fetch(`/api/sessions/${sess.id}/messages`),
        fetch(`/api/sessions/${sess.id}/patches?status=pending`),
        fetch(`/api/sessions/${sess.id}/commands?status=pending`),
      ]);
      const data = await messagesResponse.json();
      if (messagesResponse.ok) setMessages((data.messages || []).map(hydrateMessage));
      const patchData = await patchesResponse.json();
      setPendingPatches(patchesResponse.ok ? (patchData.patches || []) : []);
      const commandData = await commandsResponse.json();
      setPendingCommands(commandsResponse.ok ? (commandData.commands || []) : []);
    } catch (e) {
      console.error(e);
    }
  }

  useEffect(() => {
    if (activeSession && currentModel) refreshContext();
  }, [activeSession, currentModel]);

  async function refreshContext() {
    if (!activeSession) return;
    try {
      const res = await fetch(`/api/context?session_id=${activeSession.id}&model=${currentModel}`);
      const data = await res.json();
      setContextData({
        pct: Math.round(data.usage_pct * 100),
        zone: data.zone || 'green',
        tokensUsed: data.tokens_used || 0,
        tokensTotal: data.tokens_total || 0,
      });
    } catch (e) {
      console.error(e);
    }
  }

  const handleSend = async (customPrompt) => {
    const promptText = typeof customPrompt === 'string' ? customPrompt : input.trim();
    if (!promptText || agentRunning || !activeSession) return;

    const activeFileContent = includeFile && activeFilePath && window.getCurrentContent
      ? window.getCurrentContent()
      : '';

    // The assistant placeholder is pushed immediately (not after the fetch
    // resolves) so there's something on screen — a running timeline row —
    // the instant the user hits send, instead of a dead gap until the
    // network round trip and any backend context-resolution work finishes.
    setMessages(prev => [...prev, { role: 'user', content: promptText }, { role: 'assistant', content: '', timeline: [] }]);
    setInput('');
    setPendingPatches([]);
    setPendingCommands([]);
    setAgentRunning(true);
    const estimatedPromptTokens = Math.ceil((promptText.length + activeFileContent.length) / 4);
    streamStateRef.current = { assistantText: '', completionChars: 0, thinkingChars: 0 };
    setLiveUsage({ prompt: estimatedPromptTokens, completion: 0 });

    const controller = new AbortController();
    abortControllerRef.current = controller;

    try {
      const response = await fetch('/api/chat', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          prompt: promptText,
          model: currentModel,
          session_id: activeSession.id,
          think: thinkMode,
          think_level: thinkLevel,
          // Always send the full editor context (file path, line, col, selection)
          // so the AI always knows exactly where the user is — even when
          // active file content is excluded from the prompt.
          editor_context: editorContext ? { ...editorContext, active_file_content: activeFileContent } : null,
        }),
        signal: controller.signal,
      });

      if (response.status === 429) {
        // Replace the placeholder assistant bubble (rather than leaving an
        // empty one behind) with the actual error.
        setMessages(prev => [...prev.slice(0, -1), { role: 'system', content: "Error: Context full. Summarizing..." }]);
        setAgentRunning(false);
        refreshContext();
        return;
      }
      if (!response.ok || !response.body) {
        throw new Error(`Chat request failed with status ${response.status}`);
      }

      const reader = response.body.getReader();
      const decoder = new TextDecoder("utf-8");

      let sseBuffer = '';
      while (true) {
        const { value, done } = await reader.read();
        if (done) break;

        sseBuffer += decoder.decode(value, { stream: true });
        const lines = sseBuffer.split('\n\n');
        sseBuffer = lines.pop() || '';

        for (let line of lines) {
          if (!line.startsWith('data: ')) continue;
          try {
            const parsed = JSON.parse(line.substring(6));
            if (parsed.done) break;
            const { type, data } = parsed;

            if (type === 'token') {
              streamStateRef.current.assistantText += data.text;
              streamStateRef.current.completionChars += data.text.length;
              setLiveUsage(prev => ({ ...prev, completion: Math.ceil((streamStateRef.current.completionChars + streamStateRef.current.thinkingChars) / 4) }));
            } else if (type === 'replace_content') {
              streamStateRef.current.assistantText = data.text || '';
            } else if (type === 'thinking_token') {
              streamStateRef.current.thinkingChars += data.text.length;
              setLiveUsage(prev => ({ ...prev, completion: Math.ceil((streamStateRef.current.completionChars + streamStateRef.current.thinkingChars) / 4) }));
            } else if (type === 'usage') {
              const total = data.total_tokens || 0;
              setLiveUsage({ prompt: data.prompt_tokens || 0, completion: data.completion_tokens || 0 });
              setContextData(prev => ({ ...prev, tokensUsed: total, pct: prev.tokensTotal ? Math.round((total / prev.tokensTotal) * 100) : prev.pct }));
            } else if (type === 'patch_staged') {
              setPendingPatches(previous => previous.some(patch => patch.patch_id === data.patch_id) ? previous : [...previous, data]);
            } else if (type === 'command_staged') {
              setPendingCommands(previous => previous.some(cmd => cmd.run_id === data.run_id) ? previous : [...previous, data]);
            } else if (type === 'patch_applied') {
              setAppliedPatches(previous => previous.some(patch => patch.patch_id === data.patch_id) ? previous : [...previous, data]);
            } else if (type === 'session_metadata') {
              setActiveSession(session => session?.id === data.session_id ? { ...session, title: data.title, summary: data.summary } : session);
              setSessions(items => items.map(session => session.id === data.session_id ? { ...session, title: data.title, summary: data.summary } : session));
            }

            const assistantTextSnapshot = streamStateRef.current.assistantText;

            setMessages(prev => {
              const newArr = [...prev];
              const lastMsg = { ...newArr[newArr.length - 1], timeline: [...(newArr[newArr.length - 1].timeline || [])] };
              newArr[newArr.length - 1] = lastMsg;

              switch (type) {
                case 'token':
                case 'replace_content':
                  lastMsg.content = assistantTextSnapshot;
                  lastMsg.timeline = closeRunningEntries(lastMsg.timeline);
                  break;

                case 'thinking_token': {
                  const timeline = lastMsg.timeline;
                  const last = timeline[timeline.length - 1];
                  if (last && last.kind === 'thinking' && last.status === 'running') {
                    timeline[timeline.length - 1] = { ...last, text: (last.text || '') + data.text };
                  } else {
                    timeline.push({ id: `thinking-${timeline.length}`, kind: 'thinking', status: 'running', text: data.text });
                  }
                  break;
                }

                case 'thinking_unavailable':
                  lastMsg.thinkingNotice = data.message;
                  break;

                // Without this the timeline stays empty while Ollama loads the
                // model and evaluates the prompt — which is most of the wait on
                // a cold start, and read as "nothing is happening".
                case 'agent_step': {
                  const timeline = lastMsg.timeline;
                  const last = timeline[timeline.length - 1];
                  if (last && last.kind === 'step' && last.status === 'running') {
                    timeline[timeline.length - 1] = { ...last, text: data.message || last.text };
                  } else {
                    lastMsg.timeline = closeRunningEntries(timeline);
                    lastMsg.timeline.push({
                      id: `step-${data.step ?? lastMsg.timeline.length}`,
                      kind: 'step',
                      status: 'running',
                      text: data.message || 'Working…',
                    });
                  }
                  break;
                }

                case 'tool_call':
                  lastMsg.timeline = closeRunningEntries(lastMsg.timeline);
                  lastMsg.timeline.push({ id: data.id, kind: 'tool', status: 'running', name: data.name, input: data.input });
                  break;

                case 'tool_result':
                  lastMsg.timeline = lastMsg.timeline.map(entry => entry.id === data.id
                    ? { ...entry, status: data.ok ? 'done' : 'error', output: data.output, error: data.error }
                    : entry);
                  if (data.name === 'run_terminal' && data.ok && data.output?.command) {
                    onTerminalActivity?.({ command: data.output.command, output: data.output.output });
                  }
                  break;

                case 'context_resolved':
                  lastMsg.timeline = withContextEntry(lastMsg.timeline, entry => ({
                    ...entry,
                    items: [...entry.items, { label: `${data.file}${data.symbol ? ` › ${data.symbol.name}` : ''} (${data.confidence})` }],
                    count: entry.count + 1,
                  }));
                  break;

                case 'context_candidate':
                  lastMsg.timeline = withContextEntry(lastMsg.timeline, entry => ({
                    ...entry,
                    items: [...entry.items, { label: `${data.path || data.kind}${data.symbol ? ` › ${data.symbol}` : ''}` }],
                    count: entry.count + 1,
                  }));
                  break;

                case 'context_built':
                  lastMsg.timeline = withContextEntry(lastMsg.timeline, entry => ({
                    ...entry, status: 'done', tokens: data.tokens, budget: data.budget,
                  }));
                  break;

                case 'prompt_rephrased':
                  lastMsg.timeline = [...closeRunningEntries(lastMsg.timeline), {
                    id: `rephrase-${lastMsg.timeline.length}`, kind: 'rephrase', status: 'done',
                    original: data.original, rephrased: data.rephrased,
                  }];
                  break;

                case 'follow_up':
                  lastMsg.timeline = closeRunningEntries(lastMsg.timeline);
                  lastMsg.followUp = data;
                  break;

                case 'message_saved': {
                  for (let index = newArr.length - 1; index >= 0; index -= 1) {
                    if (newArr[index].role === data.role && !newArr[index].id) {
                      newArr[index] = { ...newArr[index], id: data.id };
                      break;
                    }
                  }
                  break;
                }

                case 'usage':
                  lastMsg.usage = data;
                  break;

                case 'agent_done':
                  lastMsg.timeline = closeRunningEntries(lastMsg.timeline);
                  break;

                case 'error':
                  newArr.push({ role: 'system', content: `Agent error: ${data.message}` });
                  break;

                default:
                  break;
              }

              return newArr;
            });

            if (type === 'agent_done') setAgentRunning(false);
          } catch { /* ignore malformed SSE events */ }
        }
      }

      setAgentRunning(false);
    } catch (e) {
      if (e.name === 'AbortError') {
        setMessages(prev => {
          const next = [...prev];
          const last = next[next.length - 1];
          if (last?.role === 'assistant') {
            next[next.length - 1] = { ...last, stopped: true };
          } else {
            next.push({ role: 'system', content: 'Generation stopped.' });
          }
          return next;
        });
      } else {
        console.error(e);
        setMessages(prev => {
          const next = [...prev];
          const last = next[next.length - 1];
          // Replace the empty placeholder bubble rather than leaving it
          // behind alongside a separate error message.
          if (last?.role === 'assistant' && !last.content && !(last.timeline || []).length) {
            next[next.length - 1] = { role: 'system', content: "Error connecting to backend." };
          } else {
            next.push({ role: 'system', content: "Error connecting to backend." });
          }
          return next;
        });
      }
    } finally {
      abortControllerRef.current = null;
      setAgentRunning(false);
    }
  };

  const handleStop = () => abortControllerRef.current?.abort();

  const handleCopy = async (content) => {
    try { await navigator.clipboard.writeText(content); } catch (error) { console.error(error); }
  };

  const handleReinsert = (content) => {
    setInput(content);
    requestAnimationFrame(() => inputRef.current?.focus());
  };

  const handleSaveEdit = async (messageIndex) => {
    const message = messages[messageIndex];
    const content = editDraft.trim();
    if (!content) return;
    if (message.id) {
      const res = await fetch(`/api/messages/${message.id}`, {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ content }),
      });
      if (!res.ok) {
        window.alert('Could not update this message.');
        return;
      }
    }
    setMessages(prev => prev.map((item, index) => index === messageIndex ? { ...item, content } : item));
    setEditingMessage(null);
    refreshContext();
  };

  const submitFollowUp = (messageIndex, followUp) => {
    const rawAnswer = followUpAnswers[messageIndex];
    const answer = Array.isArray(rawAnswer) ? rawAnswer.join(', ') : (rawAnswer || '').trim();
    if (!answer) return;
    setMessages(prev => prev.map((message, index) => index === messageIndex
      ? { ...message, followUp: { ...message.followUp, answered: true, answer } }
      : message));
    handleSend(`Regarding your question "${followUp.question}", my answer is: ${answer}`);
  };

  const resolvePatches = async (action, patchIDs) => {
    if (!activeSession || patchIDs.length === 0) return;
    const response = await fetch(`/api/sessions/${activeSession.id}/patches/${action}`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ patch_ids: patchIDs }),
    });
    const data = await response.json().catch(() => ({}));
    if (!response.ok) throw new Error(data.message || `Could not ${action} patches`);
    const resolved = action === 'approve' ? (data.applied || []) : (data.rejected || patchIDs);
    const resolvedPatches = pendingPatches.filter(patch => resolved.includes(patch.patch_id));
    setPendingPatches(previous => previous.filter(patch => !resolved.includes(patch.patch_id)));
    if (action === 'approve' && resolved.length > 0) {
      onFilesChanged?.();
      if (resolvedPatches.some(patch => sameWorkspacePath(patch.file_path, activeFilePath))) {
        await window.reloadCurrentFile?.();
      }
    }
    if (data.failed?.length) {
      throw new Error(data.failed.map(item => `${item.patch_id}: ${item.error}`).join('\n'));
    }
  };

  const handleApprovePatch = patchID => resolvePatches('approve', [patchID]);
  const handleRejectPatch = patchID => resolvePatches('reject', [patchID]);
  const handleApproveAll = () => resolvePatches('approve', pendingPatches.map(patch => patch.patch_id));
  const handleRejectAll = () => resolvePatches('reject', pendingPatches.map(patch => patch.patch_id));

  // A run_terminal change is already on disk by the time it shows up here —
  // Keep just dismisses the notice (nothing left to write), Undo reverts it
  // via the same rollback endpoint an applied create_file/apply_patch change
  // already uses.
  const handleKeepAppliedPatch = async patchID => {
    setAppliedPatches(previous => previous.filter(patch => patch.patch_id !== patchID));
  };
  const handleUndoAppliedPatch = async patchID => {
    const response = await fetch(`/api/patches/${patchID}/rollback`, { method: 'POST' });
    const data = await response.json().catch(() => ({}));
    if (!response.ok) throw new Error(data.message || 'Could not undo this change');
    setAppliedPatches(previous => previous.filter(patch => patch.patch_id !== patchID));
    onFilesChanged?.();
    await window.reloadCurrentFile?.();
  };

  const resolveCommands = async (action, runIDs) => {
    if (!activeSession || runIDs.length === 0) return;
    const response = await fetch(`/api/sessions/${activeSession.id}/commands/${action}`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ run_ids: runIDs }),
    });
    const data = await response.json().catch(() => ({}));
    if (!response.ok) throw new Error(data.message || `Could not ${action} commands`);
    if (action === 'approve') {
      const results = data.completed || [];
      if (results.length > 0) {
        setPendingCommands(previous => previous.map(cmd => {
          const result = results.find(r => r.run_id === cmd.run_id);
          return result ? { ...cmd, status: result.status, output: result.output } : cmd;
        }));
        // A completed install/scaffold command can add or change many files
        // at once — let the sidebar and open buffers know to refresh.
        if (results.some(r => r.status === 'completed')) {
          onFilesChanged?.();
          await window.reloadCurrentFile?.();
        }
      }
    } else {
      const rejected = data.rejected || runIDs;
      setPendingCommands(previous => previous.filter(cmd => !rejected.includes(cmd.run_id)));
    }
    if (data.failed?.length) {
      throw new Error(data.failed.map(item => `${item.run_id}: ${item.error}`).join('\n'));
    }
  };

  const handleApproveCommand = runID => resolveCommands('approve', [runID]);
  const handleRejectCommand = runID => resolveCommands('reject', [runID]);
  const handleApproveAllCommands = () => resolveCommands('approve', pendingCommands.filter(cmd => !cmd.status || cmd.status === 'pending').map(cmd => cmd.run_id));
  const handleRejectAllCommands = () => resolveCommands('reject', pendingCommands.filter(cmd => !cmd.status || cmd.status === 'pending').map(cmd => cmd.run_id));

  const hasFollowUpAnswer = (messageIndex) => {
    const answer = followUpAnswers[messageIndex];
    return Array.isArray(answer) ? answer.length > 0 : Boolean((answer || '').trim());
  };

  useEffect(() => {
    if (messagesEndRef.current) messagesEndRef.current.scrollIntoView({ behavior: 'smooth' });
  }, [messages]);

  if (!activeProject) {
    return <div style={{ padding: 16, textAlign: 'center', color: 'var(--studio-muted, #746b63)' }}>No project selected</div>;
  }

  const activeFile = editorContext?.active_file || '';
  const selectedSymbol = editorContext?.selected_symbol;
  const contextBadgeLabel = selectedSymbol
    ? `${selectedSymbol.kind} ${selectedSymbol.name}`
    : activeFile ? activeFile.split('/').pop() : null;

  return (
    <ConfigProvider theme={{
      components: {
        Select: {
          colorBgContainer: 'var(--studio-panel, #f5f1e9)',
          colorBorder: 'var(--studio-border, #d8d1c5)',
          colorText: 'var(--studio-text, #2f2a26)',
          colorIcon: 'var(--studio-muted, #746b63)',
          colorTextPlaceholder: 'var(--studio-subtle, #91877e)',
        }
      }
    }}>
      <div style={{ display: 'flex', flexDirection: 'column', height: '100%', width: '100%', backgroundColor: 'var(--studio-bg, #f7f4ed)' }}>

        <div style={{ position: 'relative', borderBottom: '1px solid var(--studio-border, #d8d1c5)' }}>
          <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 8, padding: '6px 12px' }}>
            <button
              type="button"
              onClick={() => setSessionsOpen(open => !open)}
              title="Switch conversation"
              style={{ minWidth: 0, flex: 1, padding: 0, display: 'flex', alignItems: 'center', gap: 6, color: 'var(--studio-text, #2f2a26)', border: 0, background: 'transparent', cursor: 'pointer', textAlign: 'left' }}
            >
              <StarFilled style={{ flexShrink: 0, fontSize: 12, color: 'var(--studio-accent, #c15f3c)' }} />
              <Text strong ellipsis style={{ minWidth: 0, color: 'var(--studio-text, #2f2a26)', fontSize: '12px' }}>
                {activeSession?.title || 'New Conversation'}
              </Text>
              <DownOutlined style={{ flexShrink: 0, color: 'var(--studio-subtle, #91877e)', fontSize: 8 }} />
            </button>
            <Tooltip title={`Context used: ${(contextData.tokensUsed || 0).toLocaleString()} / ${(contextData.tokensTotal || 0).toLocaleString()} tokens${(agentRunning || liveUsage.prompt > 0 || liveUsage.completion > 0) ? ` · this turn +${liveUsage.prompt.toLocaleString()} in / ${liveUsage.completion.toLocaleString()} out` : ''}`}>
              <span style={{
                flexShrink: 0, fontSize: 9, fontWeight: 700, padding: '1px 6px', borderRadius: 8, lineHeight: '14px',
                color: contextData.pct > 80 ? 'var(--studio-danger, #b85c5c)' : contextData.pct > 50 ? 'var(--studio-warning, #c69455)' : 'var(--studio-success, #6b8f71)',
                backgroundColor: contextData.pct > 80
                  ? 'color-mix(in srgb, var(--studio-danger, #b85c5c) 12%, transparent)'
                  : contextData.pct > 50
                    ? 'color-mix(in srgb, var(--studio-warning, #c69455) 12%, transparent)'
                    : 'color-mix(in srgb, var(--studio-success, #6b8f71) 12%, transparent)',
              }}>
                {contextData.pct}%
              </span>
            </Tooltip>
            <Button type="text" icon={<PlusOutlined style={{ fontSize: 11, color: 'var(--studio-muted, #746b63)' }} />} size="small" style={{ width: 22, height: 22, minWidth: 22 }} onClick={handleNewSession} title="New conversation" />
          </div>
          <div style={{ position: 'absolute', left: 0, right: 0, bottom: -1, height: 1.5, backgroundColor: 'var(--studio-border, #d8d1c5)' }}>
            <div style={{
              height: '100%', width: `${Math.min(contextData.pct, 100)}%`, transition: 'width 0.3s ease',
              backgroundColor: contextData.pct > 80 ? 'var(--studio-danger, #b85c5c)' : contextData.pct > 50 ? 'var(--studio-warning, #c69455)' : 'var(--studio-success, #6b8f71)',
            }} />
          </div>
        </div>

        <div style={{ padding: sessionsOpen ? '10px 16px 0' : '0 16px' }}>
          {sessionsOpen && (
            <div style={{ backgroundColor: 'var(--studio-surface, #fffdf8)', border: '1px solid var(--studio-border, #d8d1c5)', borderRadius: 6, marginBottom: 16, maxHeight: 150, overflowY: 'auto' }}>
              {sessions.map(s => (
                <div
                  key={s.id}
                  onClick={() => selectSession(s)}
                  style={{
                    padding: '8px 12px', cursor: 'pointer',
                    backgroundColor: activeSession?.id === s.id ? 'var(--studio-border, #d8d1c5)' : 'transparent',
                    color: 'var(--studio-text, #2f2a26)', fontSize: '13px', display: 'flex',
                    alignItems: 'center', justifyContent: 'space-between', gap: 8,
                  }}
                  onMouseOver={(e) => { if (activeSession?.id !== s.id) e.currentTarget.style.backgroundColor = 'rgba(193,95,60,0.08)' }}
                  onMouseOut={(e) => { if (activeSession?.id !== s.id) e.currentTarget.style.backgroundColor = 'transparent' }}
                >
                  <span style={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{s.title}</span>
                  <Popconfirm
                    title="Delete this conversation?"
                    description="Its messages will be permanently removed."
                    okText="Delete"
                    okButtonProps={{ danger: true }}
                    onConfirm={(e) => { e?.stopPropagation(); handleDeleteSession(s); }}
                    onCancel={(e) => e?.stopPropagation()}
                  >
                    <Button type="text" danger size="small" aria-label={`Delete ${s.title}`} icon={<DeleteOutlined />} disabled={agentRunning} onClick={(e) => e.stopPropagation()} />
                  </Popconfirm>
                </div>
              ))}
            </div>
          )}
        </div>

        <div style={{ flexGrow: 1, overflowY: 'auto', padding: 16, display: 'flex', flexDirection: 'column' }}>
          {messages.length === 0 ? (
            <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', justifyContent: 'center', height: '100%', gap: 16, padding: '24px 0' }}>
              <MessageOutlined style={{ fontSize: 48, color: 'var(--studio-border, #d8d1c5)' }} />
              <Text style={{ color: 'var(--studio-text, #2f2a26)', fontSize: '16px', fontWeight: 600 }}>Start a conversation</Text>
              <Text style={{ color: 'var(--studio-muted, #746b63)', fontSize: '13px', textAlign: 'center', marginBottom: 16 }}>
                Ask about your code, get explanations, or request changes.
              </Text>
              <div style={{ display: 'flex', flexDirection: 'column', gap: 8, width: '100%' }}>
                <Button style={{ backgroundColor: 'transparent', borderColor: 'var(--studio-border, #d8d1c5)', color: 'var(--studio-text, #2f2a26)', justifyContent: 'flex-start', padding: '0 12px', height: 36 }} icon={<BulbOutlined style={{ color: 'var(--studio-muted, #746b63)' }} />} onClick={() => handleSend('Explain this file')}>Explain this file</Button>
                <Button style={{ backgroundColor: 'transparent', borderColor: 'var(--studio-border, #d8d1c5)', color: 'var(--studio-text, #2f2a26)', justifyContent: 'flex-start', padding: '0 12px', height: 36 }} icon={<CodeOutlined style={{ color: 'var(--studio-muted, #746b63)' }} />} onClick={() => handleSend('Review dependencies')}>Review dependencies</Button>
                <Button style={{ backgroundColor: 'transparent', borderColor: 'var(--studio-border, #d8d1c5)', color: 'var(--studio-text, #2f2a26)', justifyContent: 'flex-start', padding: '0 12px', height: 36 }} icon={<SafetyCertificateOutlined style={{ color: 'var(--studio-muted, #746b63)' }} />} onClick={() => handleSend('Find security issues')}>Find security issues</Button>
              </div>
            </div>
          ) : (
            messages.flatMap((msg, i) => {
              const isUser = msg.role === 'user';
              const isSystem = msg.role === 'system';
              const isEditing = editingMessage === i;
              const isLastMessage = i === messages.length - 1;
              const isRunningHere = agentRunning && isLastMessage;

              // A real timeline once the agent has emitted steps; otherwise a
              // single placeholder row while it's working but hasn't streamed
              // anything yet (covers the model-load/prompt-eval stretch on a
              // cold start) — never both, and never once real content exists.
              const rawTimeline = msg.role === 'assistant' ? (msg.timeline || []) : [];
              const stepEntries = rawTimeline.length > 0
                ? rawTimeline
                : (msg.role === 'assistant' && isRunningHere && !msg.content ? [workingPlaceholderEntry()] : []);
              const hasSteps = stepEntries.length > 0;

              // Role-specific content/edit UI/follow-up/action-buttons —
              // attached to whichever row is visually last for this message
              // (the header row if there are no steps, otherwise the final
              // step row), so it reads as part of the same list instead of a
              // separate block glued on afterward.
              const messageBody = (
                <>
                  {msg.role === 'assistant' ? (
                    isEditing ? (
                      <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
                        <TextArea autoSize={{ minRows: 2, maxRows: 8 }} value={editDraft} onChange={event => setEditDraft(event.target.value)} />
                        <div style={{ display: 'flex', gap: 4 }}>
                          <Button size="small" type="primary" icon={<CheckOutlined />} onClick={() => handleSaveEdit(i)}>Save</Button>
                          <Button size="small" icon={<CloseOutlined />} onClick={() => setEditingMessage(null)}>Cancel</Button>
                        </div>
                      </div>
                    ) : msg.content && (
                      <div style={{ whiteSpace: 'pre-wrap', fontSize: '13.6px', lineHeight: 1.5, color: 'var(--studio-text, #2f2a26)' }}>
                        <MessageContent content={msg.content} />
                        {isRunningHere && !stepEntries.some(e => e.status === 'running') && (
                          <span style={{ display: 'inline-block', width: '2px', height: '1em', backgroundColor: 'var(--studio-accent, #c15f3c)', marginLeft: 1, verticalAlign: 'text-bottom', animation: 'blink 1s step-end infinite' }} aria-hidden="true" />
                        )}
                      </div>
                    )
                  ) : isEditing ? (
                    <div style={{ minWidth: 260, display: 'flex', flexDirection: 'column', gap: 6 }}>
                      <TextArea autoSize={{ minRows: 2, maxRows: 8 }} value={editDraft} onChange={event => setEditDraft(event.target.value)} />
                      <div style={{ display: 'flex', gap: 4 }}>
                        <Button size="small" type="primary" icon={<CheckOutlined />} onClick={() => handleSaveEdit(i)}>Save</Button>
                        <Button size="small" icon={<CloseOutlined />} onClick={() => setEditingMessage(null)}>Cancel</Button>
                      </div>
                    </div>
                  ) : (
                    <div style={{ whiteSpace: 'pre-wrap', fontSize: isSystem ? '12px' : '13.6px', lineHeight: 1.4 }}>{msg.content}</div>
                  )}

                  {msg.role === 'assistant' && msg.followUp && !msg.followUp.answered && (
                    <div style={{ marginTop: 10, padding: 12, border: '1px solid var(--studio-border, #c8bfb2)', borderRadius: 8, backgroundColor: 'var(--studio-surface, #fffdf8)' }}>
                      <Text style={{ color: 'var(--studio-text, #2f2a26)', display: 'block', marginBottom: 10 }}>{msg.followUp.question}</Text>
                      <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'stretch', gap: 10 }}>
                        {msg.followUp.input_type === 'multiselect' && (msg.followUp.options || []).length > 0 ? (
                          <Checkbox.Group
                            value={followUpAnswers[i] || []}
                            onChange={value => setFollowUpAnswers(prev => ({ ...prev, [i]: value }))}
                            style={{ display: 'flex', flexDirection: 'column', gap: 7 }}
                            options={msg.followUp.options.map(option => ({ value: option, label: option }))}
                          />
                        ) : msg.followUp.input_type === 'select' && (msg.followUp.options || []).length > 0 ? (
                          <Radio.Group
                            value={followUpAnswers[i]}
                            onChange={event => setFollowUpAnswers(prev => ({ ...prev, [i]: event.target.value }))}
                            style={{ display: 'flex', flexDirection: 'column', gap: 7 }}
                            options={msg.followUp.options.map(option => ({ value: option, label: option }))}
                          />
                        ) : (
                          <BorderBeam color="var(--studio-accent, #c15f3c)" style={{ opacity: followUpFocusedIndex === i ? 1 : 0, transition: 'opacity 0.25s ease' }}>
                            <div style={{ position: 'relative' }}>
                              <Input
                                placeholder="Type your answer"
                                value={followUpAnswers[i] || ''}
                                onChange={event => setFollowUpAnswers(prev => ({ ...prev, [i]: event.target.value }))}
                                onPressEnter={() => submitFollowUp(i, msg.followUp)}
                                onFocus={() => setFollowUpFocusedIndex(i)}
                                onBlur={() => setFollowUpFocusedIndex(current => current === i ? null : current)}
                              />
                            </div>
                          </BorderBeam>
                        )}
                        <Button type="primary" disabled={agentRunning || !hasFollowUpAnswer(i)} onClick={() => submitFollowUp(i, msg.followUp)}>Continue</Button>
                      </div>
                    </div>
                  )}

                  {msg.role === 'assistant' && msg.followUp?.answered && (
                    <Text style={{ display: 'block', marginTop: 8, color: 'var(--studio-muted, #746b63)', fontSize: 11 }}>Answered: {msg.followUp.answer}</Text>
                  )}

                  {!isSystem && msg.content && !isEditing && (
                    <div style={{ display: 'flex', justifyContent: 'flex-start', gap: 2, marginTop: 5 }}>
                      <Button type="text" size="small" title="Edit" aria-label="Edit message" icon={<EditOutlined />} onClick={() => { setEditingMessage(i); setEditDraft(msg.content); }} />
                      <Button type="text" size="small" title="Copy" aria-label="Copy message" icon={<CopyOutlined />} onClick={() => handleCopy(msg.content)} />
                      <Button type="text" size="small" title="Insert into prompt" aria-label="Insert into prompt" icon={<RedoOutlined />} onClick={() => handleReinsert(msg.content)} />
                    </div>
                  )}

                  {msg.stopped && <Text style={{ display: 'block', marginTop: 5, color: 'var(--studio-danger, #b85c5c)', fontSize: 11 }}>Stopped</Text>}
                </>
              );

              const rows = [];

              rows.push(
                <Row
                  key={`${i}-header`}
                  drawConnector={hasSteps || !isLastMessage}
                  spacious={!hasSteps}
                  dot={<RoleDot
                    color={isUser ? 'var(--studio-muted, #9b9085)' : isSystem ? 'var(--studio-border, #d8d1c5)' : 'var(--studio-accent, #c15f3c)'}
                    pulsing={msg.role === 'assistant' && isRunningHere}
                  />}
                >
                  <div style={{
                    color: isSystem ? 'var(--studio-subtle, #91877e)' : 'var(--studio-text, #2f2a26)',
                    fontStyle: isSystem ? 'italic' : 'normal',
                  }}>
                    <Text style={{ fontWeight: 600, display: 'block', marginBottom: 4, color: 'var(--studio-muted, #746b63)', fontSize: 12 }}>
                      {isUser ? 'You' : isSystem ? 'System' : 'Assistant'}
                    </Text>
                    {msg.role === 'assistant' && msg.thinkingNotice && (
                      <div style={{ marginBottom: 8, color: 'var(--studio-warning, #94613f)', fontSize: 11 }}>{msg.thinkingNotice}</div>
                    )}
                    {!hasSteps && messageBody}
                  </div>
                </Row>
              );

              stepEntries.forEach((entry, idx) => {
                const isFinalStep = idx === stepEntries.length - 1;
                const overrideKey = `${i}:${entry.id}`;
                const expanded = overrideKey in timelineOverrides ? timelineOverrides[overrideKey] : (isRunningHere && isFinalStep);
                rows.push(
                  <Row
                    key={`${i}-${entry.id}`}
                    drawConnector={!isFinalStep || !isLastMessage}
                    spacious={isFinalStep}
                    dot={<StepDot entry={entry} />}
                  >
                    <TimelineEntryContent
                      entry={entry}
                      expanded={expanded}
                      onToggle={() => setTimelineOverrides(prev => ({ ...prev, [overrideKey]: !expanded }))}
                    />
                    {isFinalStep && <div style={{ marginTop: 8 }}>{messageBody}</div>}
                  </Row>
                );
              });

              return rows;
            })
          )}
          {pendingPatches.length > 0 && !agentRunning && (
            <PatchReview patches={pendingPatches} onApprove={handleApprovePatch} onReject={handleRejectPatch} onApproveAll={handleApproveAll} onRejectAll={handleRejectAll} />
          )}
          {pendingCommands.length > 0 && !agentRunning && (
            <CommandReview commands={pendingCommands} onApprove={handleApproveCommand} onReject={handleRejectCommand} onApproveAll={handleApproveAllCommands} onRejectAll={handleRejectAllCommands} />
          )}
          {appliedPatches.length > 0 && !agentRunning && (
            <AppliedPatchReview patches={appliedPatches} onKeep={handleKeepAppliedPatch} onUndo={handleUndoAppliedPatch} />
          )}
          <div ref={messagesEndRef} />
        </div>

        <div style={{ padding: '16px 20px', borderTop: '1px solid var(--studio-border, #d8d1c5)', display: 'flex', flexDirection: 'column', gap: 12 }}>
          <BorderBeam color="var(--studio-accent, #c15f3c)" style={{ opacity: promptFocused ? 1 : 0, transition: 'opacity 0.25s ease' }}>
          <div style={{ position: 'relative', backgroundColor: 'var(--studio-surface, #fffdf8)', border: '1px solid var(--studio-border, #d8d1c5)', borderRadius: 'var(--studio-radius, 8px)', padding: '4px', display: 'flex', flexDirection: 'column', boxShadow: '0 4px 16px rgba(0,0,0,0.04)' }}>

            {includeFile && contextBadgeLabel && (
               <div style={{ padding: '8px 16px 0 16px', display: 'flex', alignItems: 'center', gap: 6, fontSize: '11px', color: 'var(--studio-muted, #746b63)' }}>
                 <FileOutlined />
                 <span>Current file <strong>{contextBadgeLabel}</strong> will be sent in context</span>
                 <CloseOutlined style={{ cursor: 'pointer', padding: 4 }} onClick={() => setIncludeFile(false)} title="Detach file" />
               </div>
            )}

            <TextArea
              ref={inputRef}
              autoSize={{ minRows: 1, maxRows: 6 }}
              placeholder="Describe your task or prompt..."
              value={input}
              onChange={e => setInput(e.target.value)}
              onFocus={() => setPromptFocused(true)}
              onBlur={() => setPromptFocused(false)}
              onKeyDown={e => {
                if ((e.ctrlKey || e.metaKey) && e.key === 'Enter') {
                  e.preventDefault();
                  handleSend();
                }
              }}
              style={{ backgroundColor: 'transparent', border: 'none', boxShadow: 'none', color: 'var(--studio-text, #2f2a26)', resize: 'none', padding: '12px 16px 8px 16px', fontSize: 14 }}
            />
            
            <div style={{ display: 'flex', alignItems: 'center', gap: 8, padding: '4px 8px 8px 8px' }}>
              <Button icon={<PlusOutlined />} shape="circle" style={{ width: 32, height: 32, minWidth: 32, display: 'flex', alignItems: 'center', justifyContent: 'center', backgroundColor: 'var(--studio-surface, #fff)', border: '1px solid var(--studio-border, #e2dcd4)' }} />
              
              {!includeFile && contextBadgeLabel && (
                <Button type="text" size="small" icon={<FileOutlined />} onClick={() => setIncludeFile(true)} style={{ fontSize: 12, color: 'var(--studio-muted, #746b63)', padding: '0 8px' }}>
                  Include active file
                </Button>
              )}

              <span style={{ flex: 1 }} />

              <Select
                variant="borderless"
                value={currentModel || undefined}
                placeholder="Model"
                onChange={handleModelChange}
                popupMatchSelectWidth={240}
                style={{ minWidth: 0, fontSize: 13, fontWeight: 500 }}
                options={models.map(name => ({ value: name, label: name }))}
              />
              
              {isThinkingModel && (
                <Switch size="small" checked={thinkMode} onChange={setThinkMode} title="Think Mode" style={{ marginRight: 4 }} />
              )}

              {agentRunning ? (
                <Button danger type="primary" shape="circle" title="Stop generation" aria-label="Stop generation" icon={<CloseOutlined style={{ fontSize: 14 }} />} onClick={handleStop} style={{ width: 34, minWidth: 34, height: 34, display: 'flex', alignItems: 'center', justifyContent: 'center' }} />
              ) : (
                <Button
                  type="primary"
                  shape="circle"
                  icon={<ArrowUpOutlined style={{ fontSize: 16 }} />}
                  onClick={() => handleSend()}
                  disabled={!input.trim()}
                  style={{ width: 34, minWidth: 34, height: 34, display: 'flex', alignItems: 'center', justifyContent: 'center', backgroundColor: input.trim() ? 'var(--studio-text, #2f2a26)' : 'var(--studio-panel, #e2dcd4)', borderColor: 'transparent', color: input.trim() ? 'var(--studio-bg, #fff)' : 'var(--studio-subtle, #91877e)' }}
                />
              )}
            </div>
          </div>
          </BorderBeam>
        </div>
      </div>
    </ConfigProvider>
  );
}
