import React from 'react';
import { Modal, Select, Slider, Switch, Typography, Button } from 'antd';
import { FONT_FAMILIES, DEFAULT_FONT_SETTINGS } from '../state/fontSettings';

const { Text, Title } = Typography;

const PREVIEW_CODE = `function greet(name) {
  const message = \`Hello, \${name}!\`;
  return message !== null && message.length >= 0;
}`;

function Row({ label, hint, children }) {
  return (
    <div style={{ marginBottom: 20 }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'baseline', marginBottom: 8 }}>
        <Text strong style={{ fontSize: 13 }}>{label}</Text>
        {hint && <Text type="secondary" style={{ fontSize: 12 }}>{hint}</Text>}
      </div>
      {children}
    </div>
  );
}

export default function FontSettingsModal({ open, settings, onChange, onClose }) {
  const family = FONT_FAMILIES.find(f => f.id === settings.fontFamilyId) || FONT_FAMILIES[0];

  const reset = () => onChange(DEFAULT_FONT_SETTINGS);

  return (
    <Modal
      className="font-settings"
      title="Editor Font"
      width={560}
      open={open}
      onCancel={onClose}
      footer={[
        <Button key="reset" onClick={reset}>Reset to defaults</Button>,
        <Button key="done" type="primary" onClick={onClose}>Done</Button>,
      ]}
      destroyOnHidden
    >
      <Row label="Font Family">
        <Select
          style={{ width: '100%' }}
          value={settings.fontFamilyId}
          onChange={fontFamilyId => onChange({ fontFamilyId })}
          options={FONT_FAMILIES.map(f => ({ value: f.id, label: f.label }))}
        />
      </Row>

      <Row label="Font Size" hint={`${settings.fontSize}px`}>
        <Slider min={10} max={24} step={1} value={settings.fontSize} onChange={fontSize => onChange({ fontSize })} />
      </Row>

      <Row label="Line Height" hint={`${settings.lineHeight.toFixed(1)}×`}>
        <Slider min={1.0} max={2.2} step={0.1} value={settings.lineHeight} onChange={lineHeight => onChange({ lineHeight })} />
      </Row>

      <Row label="Letter Spacing" hint={`${settings.letterSpacing.toFixed(1)}px`}>
        <Slider min={-1} max={3} step={0.1} value={settings.letterSpacing} onChange={letterSpacing => onChange({ letterSpacing })} />
      </Row>

      <Row
        label="Font Ligatures"
        hint={!family.ligatures ? 'This font has no ligature glyphs' : undefined}
      >
        <Switch checked={settings.ligatures} onChange={ligatures => onChange({ ligatures })} />
      </Row>

      <Title level={5} style={{ marginTop: 24, marginBottom: 8 }}>Preview</Title>
      <pre
        style={{
          margin: 0,
          padding: 16,
          borderRadius: 8,
          background: 'var(--studio-panel, #f5f1e9)',
          border: '1px solid var(--studio-border, #d8d1c5)',
          color: 'var(--studio-text, #2f2a26)',
          overflowX: 'auto',
          fontFamily: family.value,
          fontSize: settings.fontSize,
          lineHeight: settings.lineHeight,
          letterSpacing: `${settings.letterSpacing}px`,
          fontVariantLigatures: settings.ligatures ? 'common-ligatures contextual' : 'none',
          fontFeatureSettings: settings.ligatures ? '"liga" 1, "calt" 1' : '"liga" 0, "calt" 0',
        }}
      >
        {PREVIEW_CODE}
      </pre>
    </Modal>
  );
}
