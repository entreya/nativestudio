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
  CloseCircleFilled,
  LoadingOutlined,
} from '@ant-design/icons';

// Plain logic shared between ChatPanel (which owns the single conversation
// rail and needs an icon/color per step to draw its dots) and
// ThinkingTimeline (which owns each step's collapsible body). Kept in its own
// non-component module rather than exported alongside components, since
// mixing the two in one file defeats Vite Fast Refresh for that file.

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

export function iconFor(entry) {
  if (entry.status === 'error') return CloseCircleFilled;
  if (entry.status === 'running') return LoadingOutlined;
  if (entry.kind === 'thinking') return BulbOutlined;
  if (entry.kind === 'context') return SearchOutlined;
  if (entry.kind === 'step') return BulbOutlined;
  return TOOL_ICONS[entry.name] || ToolOutlined;
}

export function colorFor(entry) {
  if (entry.status === 'error') return 'var(--studio-danger, #b85c5c)';
  if (entry.status === 'running') return 'var(--studio-accent, #c15f3c)';
  return 'var(--studio-success, #6b8f71)';
}

// Placeholder entry shown while the agent is working but hasn't streamed any
// real step yet (model load / prompt eval on a cold start). ChatPanel decides
// when to inject this into its own row list — this module just owns its shape.
export function workingPlaceholderEntry() {
  return { id: 'working', kind: 'tool', name: 'working', status: 'running' };
}
