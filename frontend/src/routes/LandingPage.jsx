import React from 'react';
import { useNavigate } from 'react-router-dom';
import { Typography, Button, Card } from 'antd';
import { FolderOpenOutlined, PartitionOutlined, MessageOutlined, CodeOutlined } from '@ant-design/icons';
import { useAppState } from '../state/useAppState';
import { folderNameFromPath } from '../state/folderNameFromPath';

const { Title, Text } = Typography;

export default function LandingPage() {
  const navigate = useNavigate();
  const { studioStyle, studioClassName, projects, switchToProject, handleCreateProject } = useAppState();

  return (
    <div className={studioClassName} style={{
      ...studioStyle,
      display: 'flex', flexDirection: 'column', height: '100vh', width: '100vw',
      backgroundColor: 'var(--studio-bg, #f7f4ed)',
      alignItems: 'center', justifyContent: 'center',
    }}>
      <Card style={{
        width: 480, maxWidth: '90vw',
        backgroundColor: 'var(--studio-surface, #fffdf8)',
        border: '1px solid var(--studio-border, #d8d1c5)',
        borderRadius: 16,
        boxShadow: '0 18px 60px rgba(67,55,45,.10)',
      }} styles={{ body: { paddingTop: 40, paddingLeft: 32, paddingRight: 32 } }}>
        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'center', gap: 12, marginBottom: 4 }}>
          <CodeOutlined style={{ fontSize: 32, color: 'var(--studio-accent, #c15f3c)' }} />
          <Title level={3} style={{ margin: 0, fontWeight: 700, color: 'var(--studio-text, #2f2a26)', letterSpacing: '-0.02em' }}>
            nativestudio
          </Title>
        </div>
        <div style={{ textAlign: 'center', marginBottom: 32, color: 'var(--studio-muted, #746b63)' }}>
          Select a project or open a new folder to begin.
        </div>

        <div style={{ backgroundColor: 'var(--studio-panel, #f5f1e9)', border: '1px solid var(--studio-border, #d8d1c5)', borderRadius: 12, maxHeight: 260, overflow: 'auto' }}>
          {projects.length === 0 ? (
            <div style={{ padding: 24, textAlign: 'center', color: 'var(--studio-muted, #746b63)' }}>
              <div>No projects yet.</div>
              <div>Open a folder to get started.</div>
            </div>
          ) : (
            projects.map(p => (
              <div
                key={p.id || p.path}
                onClick={() => switchToProject(p)}
                style={{ cursor: 'pointer', padding: '12px 16px', borderBottom: '1px solid var(--studio-border, #d8d1c5)', display: 'flex', alignItems: 'flex-start', gap: 12 }}
                onMouseOver={(e) => e.currentTarget.style.backgroundColor = 'var(--studio-panel, #eee9df)'}
                onMouseOut={(e) => e.currentTarget.style.backgroundColor = 'transparent'}
              >
                <PartitionOutlined style={{ color: 'var(--studio-accent, #c15f3c)', fontSize: 20, marginTop: 2 }} />
                <div style={{ display: 'flex', flexDirection: 'column' }}>
                  <Text strong style={{ fontSize: '15px', color: 'var(--studio-text, #2f2a26)', lineHeight: 1.2 }}>{folderNameFromPath(p.path)}</Text>
                  <Text type="secondary" style={{ fontSize: '12px', color: 'var(--studio-muted, #746b63)', fontFamily: 'monospace', marginTop: 2 }}>{p.path}</Text>
                </div>
              </div>
            ))
          )}
        </div>

        <div style={{ display: 'flex', justifyContent: 'center', marginTop: 24, marginBottom: 16 }}>
          <Button type="primary" size="large" icon={<FolderOpenOutlined />} onClick={handleCreateProject} style={{ padding: '0 40px', height: 44, borderRadius: 8 }}>
            Open Folder
          </Button>
        </div>
        {projects.length > 0 && (
          <Button type="link" block icon={<MessageOutlined />} onClick={() => navigate('/conversations')}>
            View conversations & memory
          </Button>
        )}
      </Card>
    </div>
  );
}
