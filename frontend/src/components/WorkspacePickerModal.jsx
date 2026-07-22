import React, { useEffect, useMemo, useState } from 'react';
import { Alert, Breadcrumb, Button, Empty, Modal, Space, Spin, Tree, Typography } from 'antd';
import { AppstoreOutlined, FileOutlined, FolderOpenOutlined, FolderOutlined, HomeOutlined, ReloadOutlined } from '@ant-design/icons';

const { DirectoryTree } = Tree;
const { Text } = Typography;

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

const pathCrumbs = path => {
  const pieces = path.split(/[\\/]/).filter(Boolean);
  const separator = path.includes('\\') ? '\\' : '/';
  return [
    { label: 'Computer', path: separator },
    ...pieces.map((label, index) => ({ label, path: `${separator}${pieces.slice(0, index + 1).join(separator)}` })),
  ];
};

export default function WorkspacePickerModal({ open, mode = 'folder', projects = [], onCancel, onSelect }) {
  const [treeData, setTreeData] = useState([]);
  const [rootPath, setRootPath] = useState('');
  const [homePath, setHomePath] = useState('');
  const [selected, setSelected] = useState(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');

  const fetchDirectory = async path => {
    const query = new URLSearchParams({ mode });
    if (path) query.set('path', path);
    const response = await fetch(`/api/system/browse?${query}`);
    if (!response.ok) throw new Error((await response.text()) || 'Could not browse this directory');
    return response.json();
  };

  const openLocation = async path => {
    setLoading(true);
    setError('');
    setSelected(null);
    try {
      const data = await fetchDirectory(path);
      setRootPath(data.current);
      setHomePath(data.home);
      setTreeData([{
        key: data.current,
        title: data.current === data.home ? 'Home' : (data.current.split(/[\\/]/).filter(Boolean).pop() || 'Computer'),
        type: 'dir',
        isLeaf: false,
        children: (data.entries || []).map(toNode),
      }]);
    } catch (cause) {
      setTreeData([]);
      setError(cause.message);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    if (!open) return undefined;
    const timer = window.setTimeout(() => openLocation(''), 0);
    return () => window.clearTimeout(timer);
    // Opening or changing picker mode intentionally resets the browser.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, mode]);

  const loadNode = async node => {
    if (node.type !== 'dir' || node.children?.length) return;
    try {
      const data = await fetchDirectory(node.key);
      setTreeData(current => replaceChildren(current, node.key, (data.entries || []).map(toNode)));
    } catch (cause) {
      setError(cause.message);
    }
  };

  const validSelection = selected && (mode === 'folder' ? selected.type === 'dir' : selected.type === 'file');
  const crumbs = useMemo(() => pathCrumbs(rootPath), [rootPath]);

  return (
    <Modal
      className="workspace-picker"
      title={mode === 'folder' ? 'Open folder' : 'Open file'}
      open={open}
      width={800}
      okText={mode === 'folder' ? 'Open folder' : 'Open file'}
      okButtonProps={{ disabled: !validSelection }}
      onOk={() => validSelection && onSelect(selected.key)}
      onCancel={onCancel}
      destroyOnHidden
    >
      <div className="workspace-picker-toolbar">
        <Breadcrumb items={crumbs.map(crumb => ({
          title: <button type="button" onClick={() => openLocation(crumb.path)}>{crumb.label}</button>,
        }))} />
        <Button type="text" size="small" icon={<ReloadOutlined />} onClick={() => openLocation(rootPath)} aria-label="Refresh directory" />
      </div>
      <div className="workspace-picker-body">
        <aside className="workspace-picker-locations">
          <button type="button" onClick={() => openLocation(homePath || '')}><HomeOutlined /> Home</button>
          <button type="button" onClick={() => openLocation('/')}><AppstoreOutlined /> Computer</button>
          {projects.slice(0, 8).map(project => (
            <button type="button" key={project.id || project.path} title={project.path} onClick={() => openLocation(project.path)}>
              <FolderOutlined /> <span>{project.name || project.path.split(/[\\/]/).pop()}</span>
            </button>
          ))}
        </aside>
        <section className="workspace-picker-tree">
          {error && <Alert type="error" showIcon message={error} closable onClose={() => setError('')} />}
          {loading ? <div className="workspace-picker-loading"><Spin /></div> : treeData.length ? (
            <DirectoryTree
              showIcon
              defaultExpandAll
              treeData={treeData}
              loadData={loadNode}
              selectedKeys={selected ? [selected.key] : []}
              onSelect={(_, info) => setSelected({ key: info.node.key, type: info.node.type })}
              onDoubleClick={(_, node) => {
                if (node.type === 'file' && mode === 'file') onSelect(node.key);
              }}
              icon={node => node.type === 'file' ? <FileOutlined /> : node.expanded ? <FolderOpenOutlined /> : <FolderOutlined />}
            />
          ) : <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="No matching files or folders" />}
        </section>
      </div>
      <div className="workspace-picker-selection">
        <Text type="secondary">{validSelection ? selected.key : mode === 'folder' ? 'Select a folder' : 'Select a supported source file'}</Text>
      </div>
    </Modal>
  );
}
