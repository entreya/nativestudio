import React, { useState, useEffect } from 'react';
import { Tree, Typography } from 'antd';
import { FolderOutlined, FolderOpenOutlined, FileOutlined, DownOutlined, RightOutlined } from '@ant-design/icons';

const { Text } = Typography;

const getFileColor = (filename) => {
  if (filename.endsWith('.js') || filename.endsWith('.jsx')) return '#ca8a04';
  if (filename.endsWith('.ts') || filename.endsWith('.tsx')) return '#2563eb';
  if (filename.endsWith('.json')) return '#16a34a';
  if (filename.endsWith('.html')) return '#ea580c';
  if (filename.endsWith('.css')) return '#0284c7';
  if (filename.endsWith('.md')) return '#64748b';
  if (filename.endsWith('.go')) return '#0891b2';
  if (filename.endsWith('.php')) return '#7c3aed';
  if (filename.endsWith('.py')) return '#ca8a04';
  if (filename.endsWith('.sql')) return '#db2777';
  return '#94a3b8';
};

// Convert our fileNode JSON into antd TreeData format
const mapToTreeData = (nodes) => {
  if (!nodes) return [];
  return nodes.map(node => {
    const isDir = node.type === 'dir';
    
    return {
      title: node.name,
      key: node.path,
      isLeaf: !isDir,
      filename: node.name,
      children: mapToTreeData(node.children)
    };
  });
};

export default function FileTree({ onFileClick, refreshTrigger }) {
  const [treeData, setTreeData] = useState([]);
  const [expandedKeys, setExpandedKeys] = useState([]);
  useEffect(() => {
    fetch('/api/files')
      .then(res => res.json())
      .then(data => setTreeData(mapToTreeData(data)))
      .catch(err => console.error(err));
  }, [refreshTrigger]);

  const onSelect = (selectedKeys, info) => {
    if (info.node.isLeaf) {
      onFileClick(info.node.key);
    } else {
      // Toggle expansion for directories when clicking the title
      const key = info.node.key;
      if (expandedKeys.includes(key)) {
        setExpandedKeys(expandedKeys.filter(k => k !== key));
      } else {
        setExpandedKeys([...expandedKeys, key]);
      }
    }
  };

  const onExpand = (keys) => {
    setExpandedKeys(keys);
  };

  return (
    <div style={{ 
      width: '100%', height: '100%', overflowY: 'auto', overflowX: 'hidden',
      paddingTop: 8, paddingBottom: 16, display: 'flex', flexDirection: 'column'
    }}>
      <style>{`
        .file-tree .ant-tree-switcher {
          display: none !important;
        }
        .file-tree .ant-tree-iconEle {
          width: auto !important;
          margin-right: 6px !important;
        }
      `}</style>
      
      <Text type="secondary" style={{ 
        padding: '0 16px', marginBottom: 8, fontWeight: 700, 
        letterSpacing: 1.5, fontSize: '10.5px', textTransform: 'uppercase', display: 'none'
      }}>
        Explorer
      </Text>
      
      {treeData.length > 0 ? (
        <Tree
          className="file-tree"
          treeData={treeData}
          expandedKeys={expandedKeys}
          onExpand={onExpand}
          onSelect={onSelect}
          showIcon={true}
          blockNode={true}
          icon={(props) => {
            const filename = props.filename || props.data?.filename || props.title;
            if (props.isLeaf) {
              return (
                <span style={{ display: 'inline-flex', alignItems: 'center', gap: 6, transform: 'translateY(-1px)' }}>
                  <span style={{ width: 10, display: 'inline-block' }} />
                  <FileOutlined style={{color: getFileColor(filename)}} />
                </span>
              );
            }
            return (
              <span style={{ display: 'inline-flex', alignItems: 'center', gap: 6, transform: 'translateY(-1px)' }}>
                {props.expanded ? <DownOutlined style={{ fontSize: 10, color: 'var(--studio-subtle, #91877e)' }} /> : <RightOutlined style={{ fontSize: 10, color: 'var(--studio-subtle, #91877e)' }} />}
                {props.expanded ? <FolderOpenOutlined style={{color: 'var(--studio-muted, #746b63)'}} /> : <FolderOutlined style={{color: 'var(--studio-muted, #746b63)'}} />}
              </span>
            );
          }}
          style={{ backgroundColor: 'transparent', padding: '0 8px', fontSize: '13px' }}
        />
      ) : (
        <Text type="secondary" style={{ padding: '24px 16px', textAlign: 'center' }}>
          No files found
        </Text>
      )}
    </div>
  );
}
