import React, { useEffect, useState } from 'react';
import { Tree, Typography, Dropdown, Modal, Popover, Input, Button, App as AntApp } from 'antd';
import { FolderOutlined, FolderOpenOutlined, FileAddOutlined, FolderAddOutlined } from '@ant-design/icons';
import { FileTypeIcon } from '../fileIcons';

const { DirectoryTree } = Tree;
const { Text } = Typography;

// children stays undefined until fetched — [] means "fetched and empty",
// undefined means "not fetched yet", which is what drives DirectoryTree's
// loadData check (same convention WorkspacePickerModal uses).
const toNode = entry => ({
  key: entry.path,
  title: entry.name,
  type: entry.type,
  isLeaf: entry.type === 'file' || !entry.has_children,
  children: entry.type === 'dir' && entry.has_children ? undefined : [],
});

const replaceChildren = (nodes, key, children) => nodes.map(node => {
  if (node.key === key) return { ...node, children };
  if (node.children?.length) return { ...node, children: replaceChildren(node.children, key, children) };
  return node;
});

// Merging (rather than replacing) the root listing on refresh preserves
// already-loaded children for directories that are still there, so a
// refresh doesn't collapse/flicker the parts of the tree the user already
// expanded — only the root's own entries (new/removed files) actually change.
const mergeRootEntries = (current, entries) => {
  const existingByKey = new Map(current.map(node => [node.key, node]));
  return (entries || []).map(entry => {
    const node = toNode(entry);
    const existing = existingByKey.get(node.key);
    return existing && existing.children !== undefined ? { ...node, children: existing.children } : node;
  });
};

async function fetchDirectory(path) {
  const query = path ? `?path=${encodeURIComponent(path)}` : '';
  const response = await fetch(`/api/files${query}`);
  if (!response.ok) throw new Error((await response.text()) || 'Could not list files');
  return response.json();
}

async function postFileOp(path, body) {
  const response = await fetch(path, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
  if (!response.ok) throw new Error((await response.text()) || 'Request failed');
  return response.json();
}

const parentOf = path => {
  const idx = path.lastIndexOf('/');
  return idx <= 0 ? '' : path.slice(0, idx);
};
const joinPath = (parent, name) => (parent ? `${parent}/${name}` : `/${name}`);

/**
 * Props:
 *  - onFileClick(path): open a file in the editor
 *  - refreshTrigger: bump to force a reload (e.g. after the AI changes files)
 *  - onFileRenamed(oldPath, newPath) / onFileDeleted(path): optional — lets
 *    the editor close or retarget an open tab affected by a rename/delete
 *    made from this tree.
 *
 * Same DirectoryTree (native switcher, click-to-expand folders, showLine,
 * lazy loadData) as WorkspacePickerModal's "Open folder" browser, plus a
 * right-click context menu (and a small root toolbar) for creating,
 * renaming, and deleting files/folders directly from the explorer.
 */
export default function FileTree({ onFileClick, refreshTrigger, onFileRenamed, onFileDeleted }) {
  const [treeData, setTreeData] = useState([]);
  const [expandedKeys, setExpandedKeys] = useState([]);
  const [localTick, setLocalTick] = useState(0);
  const [createRequest, setCreateRequest] = useState(null); // { parentPath, type }
  const [createName, setCreateName] = useState('');
  const [renameRequest, setRenameRequest] = useState(null); // { path, name }
  const [renameName, setRenameName] = useState('');
  const [busy, setBusy] = useState(false);
  const [formError, setFormError] = useState('');
  const { modal, message } = AntApp.useApp();

  useEffect(() => {
    let cancelled = false;
    fetchDirectory('')
      .then(entries => { if (!cancelled) setTreeData(current => mergeRootEntries(current, entries)); })
      .catch(err => console.error(err));
    expandedKeys.forEach(key => {
      fetchDirectory(key)
        .then(entries => { if (!cancelled) setTreeData(current => replaceChildren(current, key, (entries || []).map(toNode))); })
        .catch(err => console.error(err));
    });
    return () => { cancelled = true; };
    // Intentionally only re-runs on refreshTrigger/localTick — expandedKeys is
    // read at trigger time, not tracked as a dependency (that would re-fetch
    // on every expand/collapse instead of just on an actual refresh).
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [refreshTrigger, localTick]);

  const loadNode = async node => {
    if (node.type !== 'dir' || node.children?.length) return;
    try {
      const entries = await fetchDirectory(node.key);
      setTreeData(current => replaceChildren(current, node.key, (entries || []).map(toNode)));
    } catch (err) {
      console.error(err);
    }
  };

  const onSelect = (_selectedKeys, info) => {
    if (info.node.type === 'file') onFileClick(info.node.key);
  };

  const openCreatePopover = (parentPath, type) => {
    setFormError('');
    setCreateName('');
    setCreateRequest({ parentPath, type });
  };

  const submitCreate = async () => {
    const name = createName.trim();
    if (!name) return;
    setBusy(true);
    setFormError('');
    try {
      await postFileOp('/api/files/create', { path: joinPath(createRequest.parentPath, name), type: createRequest.type });
      setCreateRequest(null);
      setLocalTick(t => t + 1);
    } catch (err) {
      setFormError(err.message);
    } finally {
      setBusy(false);
    }
  };

  const openRenameModal = (path, name) => {
    setFormError('');
    setRenameName(name);
    setRenameRequest({ path, name });
  };

  const submitRename = async () => {
    const name = renameName.trim();
    if (!name || name === renameRequest.name) {
      setRenameRequest(null);
      return;
    }
    setBusy(true);
    setFormError('');
    try {
      const newPath = joinPath(parentOf(renameRequest.path), name);
      await postFileOp('/api/files/rename', { path: renameRequest.path, new_path: newPath });
      onFileRenamed?.(renameRequest.path, newPath);
      setRenameRequest(null);
      setLocalTick(t => t + 1);
    } catch (err) {
      setFormError(err.message);
    } finally {
      setBusy(false);
    }
  };

  const confirmDelete = (path, name, type) => {
    modal.confirm({
      title: `Delete ${name}?`,
      content: type === 'dir' ? 'This folder and everything inside it will be permanently deleted.' : 'This file will be permanently deleted.',
      okText: 'Delete',
      okType: 'danger',
      onOk: async () => {
        try {
          await postFileOp('/api/files/delete', { path });
          onFileDeleted?.(path);
          setLocalTick(t => t + 1);
        } catch (err) {
          message.error(err.message || 'Could not delete this item.');
        }
      },
    });
  };

  const handleMenuClick = (key, node) => {
    if (key === 'new-file') openCreatePopover(node.key, 'file');
    else if (key === 'new-folder') openCreatePopover(node.key, 'dir');
    else if (key === 'rename') openRenameModal(node.key, node.title);
    else if (key === 'delete') confirmDelete(node.key, node.title, node.type);
  };

  const menuItemsFor = node => node.type === 'dir'
    ? [
        { key: 'new-file', label: 'New File' },
        { key: 'new-folder', label: 'New Folder' },
        { type: 'divider' },
        { key: 'rename', label: 'Rename' },
        { key: 'delete', label: 'Delete', danger: true },
      ]
    : [
        { key: 'rename', label: 'Rename' },
        { key: 'delete', label: 'Delete', danger: true },
      ];

  // Shared content for the New File / New Folder popover, whether it's
  // anchored to the root toolbar buttons or to a directory row's context menu.
  const createPopoverContent = (
    <div style={{ width: 220 }}>
      <Input
        autoFocus
        size="small"
        placeholder={createRequest?.type === 'dir' ? 'folder-name' : 'file-name.ext'}
        value={createName}
        onChange={e => setCreateName(e.target.value)}
        onPressEnter={submitCreate}
      />
      {formError && <Text type="danger" style={{ display: 'block', marginTop: 6, fontSize: 12 }}>{formError}</Text>}
      <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 6, marginTop: 8 }}>
        <Button size="small" onClick={() => setCreateRequest(null)}>Cancel</Button>
        <Button size="small" type="primary" loading={busy} disabled={!createName.trim()} onClick={submitCreate}>Create</Button>
      </div>
    </div>
  );

  const titleRender = node => {
    // Every directory row doubles as a potential popover anchor — inert
    // (open=false) for all but the one row a "New File/Folder" context-menu
    // action was just triggered on, so the popover appears right next to
    // the folder it's creating inside of rather than a detached modal.
    const isCreateAnchor = node.type === 'dir' && createRequest?.parentPath === node.key;
    return (
      <Popover
        open={isCreateAnchor}
        trigger={[]}
        placement="rightTop"
        onOpenChange={open => { if (!open) setCreateRequest(null); }}
        content={isCreateAnchor ? createPopoverContent : null}
      >
        <Dropdown
          trigger={['contextMenu']}
          menu={{ items: menuItemsFor(node), onClick: ({ key, domEvent }) => { domEvent.stopPropagation(); handleMenuClick(key, node); } }}
        >
          <span
            title={node.title}
            style={{ display: 'block', width: '100%', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}
          >
            {node.title}
          </span>
        </Dropdown>
      </Popover>
    );
  };

  return (
    <div style={{
      width: '100%', height: '100%', overflowY: 'auto', overflowX: 'hidden',
      paddingTop: 8, paddingBottom: 16, display: 'flex', flexDirection: 'column'
    }}>
      <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 2, padding: '0 8px 4px' }}>
        <Popover
          open={createRequest?.parentPath === '' && createRequest?.type === 'file'}
          trigger={[]}
          placement="bottomRight"
          onOpenChange={open => { if (!open) setCreateRequest(null); }}
          content={createRequest?.parentPath === '' && createRequest?.type === 'file' ? createPopoverContent : null}
        >
          <Button type="text" size="small" title="New file" aria-label="New file" icon={<FileAddOutlined style={{ color: 'var(--studio-muted, #746b63)' }} />} onClick={() => openCreatePopover('', 'file')} />
        </Popover>
        <Popover
          open={createRequest?.parentPath === '' && createRequest?.type === 'dir'}
          trigger={[]}
          placement="bottomRight"
          onOpenChange={open => { if (!open) setCreateRequest(null); }}
          content={createRequest?.parentPath === '' && createRequest?.type === 'dir' ? createPopoverContent : null}
        >
          <Button type="text" size="small" title="New folder" aria-label="New folder" icon={<FolderAddOutlined style={{ color: 'var(--studio-muted, #746b63)' }} />} onClick={() => openCreatePopover('', 'dir')} />
        </Popover>
      </div>

      <Text type="secondary" style={{
        padding: '0 16px', marginBottom: 8, fontWeight: 700,
        letterSpacing: 1.5, fontSize: '10.5px', textTransform: 'uppercase', display: 'none'
      }}>
        Explorer
      </Text>

      {treeData.length > 0 ? (
        <DirectoryTree
          treeData={treeData}
          expandedKeys={expandedKeys}
          onExpand={setExpandedKeys}
          loadData={loadNode}
          onSelect={onSelect}
          showIcon
          showLine={{ showLeafIcon: false }}
          titleRender={titleRender}
          icon={node => node.type === 'file'
            ? <FileTypeIcon filename={node.title || ''} />
            : node.expanded ? <FolderOpenOutlined style={{ color: 'var(--studio-muted, #746b63)' }} /> : <FolderOutlined style={{ color: 'var(--studio-muted, #746b63)' }} />}
          style={{ backgroundColor: 'transparent', padding: '0 8px', fontSize: '13px' }}
        />
      ) : (
        <Text type="secondary" style={{ padding: '24px 16px', textAlign: 'center' }}>
          No files found
        </Text>
      )}

      <Modal
        title="Rename"
        open={Boolean(renameRequest)}
        onCancel={() => setRenameRequest(null)}
        onOk={submitRename}
        okButtonProps={{ loading: busy, disabled: !renameName.trim() }}
        destroyOnHidden
      >
        <Input
          autoFocus
          value={renameName}
          onChange={e => setRenameName(e.target.value)}
          onPressEnter={submitRename}
        />
        {formError && <Text type="danger" style={{ display: 'block', marginTop: 8, fontSize: 12 }}>{formError}</Text>}
      </Modal>
    </div>
  );
}
