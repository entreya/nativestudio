import React from 'react';
import { CheckOutlined } from '@ant-design/icons';
import { Modal, Tag, Typography } from 'antd';
import { studioThemes } from '../themes';

const { Text, Title } = Typography;

export default function ThemeSettingsModal({ open, selectedTheme, onSelect, onClose }) {
  return (
    <Modal
      className="theme-settings"
      title="Appearance"
      width={860}
      open={open}
      footer={null}
      onCancel={onClose}
      destroyOnHidden
    >
      <div className="theme-settings-intro">
        <Title level={5}>Studio themes</Title>
        <Text type="secondary">Choose the original default or a theme that changes color, density, typography, geometry, and panel treatment.</Text>
      </div>
      <div className="theme-grid">
        {studioThemes.map(theme => {
          const selected = selectedTheme === theme.id;
          return (
            <button
              type="button"
              key={theme.id}
              className={`theme-card ${selected ? 'is-selected' : ''}`}
              onClick={() => onSelect(theme.id)}
              aria-pressed={selected}
            >
              <span className="theme-preview" style={{ background: theme.bg, borderColor: theme.border }}>
                <span style={{ background: theme.surface }} />
                <span style={{ background: theme.panel }} />
                <i style={{ background: theme.accent }} />
              </span>
              <span className="theme-card-copy">
                <strong>{theme.name}</strong>
                <small>{theme.description}</small>
                <span><Tag>{theme.dark ? 'Dark' : 'Light'}</Tag><Tag>{theme.layout}</Tag></span>
              </span>
              {selected && <CheckOutlined className="theme-selected-icon" />}
            </button>
          );
        })}
      </div>
    </Modal>
  );
}
