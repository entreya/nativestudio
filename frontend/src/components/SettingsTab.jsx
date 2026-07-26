import React from 'react';
import { CheckOutlined, FormatPainterOutlined, FontColorsOutlined, ThunderboltOutlined } from '@ant-design/icons';
import { Typography, Tag, Divider, Row, Col, InputNumber, Switch, Select, Slider, Tabs } from 'antd';
import { studioThemes } from '../themes';
import { useAppState } from '../state/useAppState';

const { Text, Title } = Typography;

export default function SettingsTab() {
  const { themeID, selectTheme, fontSettings, updateFontSettings, editorSettings, updateEditorSettings, agentSettings, updateAgentSettings } = useAppState();

  // Group themes by their 'pairId' so we can show Light and Dark side-by-side
  const themePairs = {};
  studioThemes.forEach(theme => {
    if (!themePairs[theme.pairId]) {
      themePairs[theme.pairId] = [];
    }
    themePairs[theme.pairId].push(theme);
  });

  const appearanceContent = (
    <div style={{ marginTop: 24 }}>
      <Title level={4} style={{ color: 'var(--studio-text)', marginBottom: 8 }}>Studio Themes</Title>
      <Text style={{ color: 'var(--studio-muted)', display: 'block', marginBottom: 24 }}>
        Choose a premium theme. Each theme provides exact brand color combinations and a unique structural layout (Bento, Glass, etc.).
      </Text>

      <div style={{ display: 'flex', flexDirection: 'column', gap: 24 }}>
        {Object.entries(themePairs).map(([pairId, themes]) => (
          <div key={pairId} style={{ display: 'flex', gap: 16 }}>
            {themes.map(theme => {
              const selected = themeID === theme.id;
              return (
                <button
                  type="button"
                  key={theme.id}
                  className={`theme-card ${selected ? 'is-selected' : ''}`}
                  style={{ flex: 1, minWidth: 0 }}
                  onClick={() => selectTheme(theme.id)}
                  aria-pressed={selected}
                >
                  <span className="theme-preview" style={{ background: theme.bg, borderColor: theme.border }}>
                    <span style={{ background: theme.surface }} />
                    <span style={{ background: theme.panel }} />
                    <i style={{ background: theme.accent }} />
                  </span>
                  <span className="theme-card-copy">
                    <strong>{theme.name}</strong>
                    <span>
                      <Tag>{theme.dark ? 'Dark' : 'Light'}</Tag>
                      <Tag>{theme.layout.charAt(0).toUpperCase() + theme.layout.slice(1)} Layout</Tag>
                    </span>
                  </span>
                  {selected && <CheckOutlined className="theme-selected-icon" />}
                </button>
              );
            })}
          </div>
        ))}
      </div>
    </div>
  );

  const typographyContent = (
    <div style={{ marginTop: 24 }}>
      <Title level={4} style={{ color: 'var(--studio-text)', marginBottom: 8 }}>
        <FontColorsOutlined style={{ marginRight: 8 }} /> Editor Typography
      </Title>
      <Text style={{ color: 'var(--studio-muted)', display: 'block', marginBottom: 24 }}>
        Configure the font and text properties of the Monaco code editor.
      </Text>

      <Row gutter={[32, 24]}>
        <Col span={12}>
          <div style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
            <Text strong style={{ color: 'var(--studio-text)' }}>Font Family</Text>
            <Select
              value={fontSettings?.fontFamilyId || 'sfmono'}
              onChange={v => updateFontSettings({ fontFamilyId: v })}
              options={[
                { value: 'sfmono', label: 'SF Mono' },
                { value: 'cascadia', label: 'Cascadia Code' },
                { value: 'fira', label: 'Fira Code' },
                { value: 'jetbrains', label: 'JetBrains Mono' },
                { value: 'menlo', label: 'Menlo' }
              ]}
            />
          </div>
        </Col>
        <Col span={12}>
          <div style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
            <Text strong style={{ color: 'var(--studio-text)' }}>Font Size (px)</Text>
            <InputNumber
              min={8} max={32}
              value={fontSettings?.fontSize || 13}
              onChange={v => updateFontSettings({ fontSize: v })}
              style={{ width: '100%' }}
            />
          </div>
        </Col>
        <Col span={12}>
          <div style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
            <Text strong style={{ color: 'var(--studio-text)' }}>Line Height (multiplier)</Text>
            <InputNumber
              min={1} max={3} step={0.1}
              value={fontSettings?.lineHeight || 1.6}
              onChange={v => updateFontSettings({ lineHeight: v })}
              style={{ width: '100%' }}
            />
          </div>
        </Col>
        <Col span={12}>
          <div style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
            <Text strong style={{ color: 'var(--studio-text)' }}>Letter Spacing (px)</Text>
            <InputNumber
              min={-2} max={10} step={0.1}
              value={fontSettings?.letterSpacing || 0}
              onChange={v => updateFontSettings({ letterSpacing: v })}
              style={{ width: '100%' }}
            />
          </div>
        </Col>
        <Col span={12}>
          <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', padding: '12px 16px', background: 'var(--studio-surface)', border: '1px solid var(--studio-border)', borderRadius: 8 }}>
            <div>
              <Text strong style={{ color: 'var(--studio-text)', display: 'block' }}>Font Ligatures</Text>
              <Text style={{ color: 'var(--studio-muted)', fontSize: 12 }}>Enable special character combinations (e.g. =&gt;, ===).</Text>
            </div>
            <Switch
              checked={fontSettings?.ligatures}
              onChange={v => updateFontSettings({ ligatures: v })}
            />
          </div>
        </Col>
      </Row>
    </div>
  );

  const editorContent = (
    <div style={{ marginTop: 24 }}>
      <Title level={4} style={{ color: 'var(--studio-text)', marginBottom: 8 }}>
        Editor Features
      </Title>
      <Text style={{ color: 'var(--studio-muted)', display: 'block', marginBottom: 24 }}>
        Configure Monaco editor behaviors.
      </Text>
      
      <Row gutter={[32, 24]}>
        {/* Toggles */}
        {[
          { key: 'minimap', label: 'Minimap', desc: 'Show the code minimap on the right side.' },
          { key: 'smoothScrolling', label: 'Smooth Scrolling', desc: 'Enable animated scrolling.' },
          { key: 'formatOnType', label: 'Format on Type', desc: 'Format code automatically as you type.' },
          { key: 'codeLens', label: 'Code Lens', desc: 'Show actionable contextual information.' },
          { key: 'bracketPairColorization', label: 'Bracket Colorization', desc: 'Colorize matching brackets.' },
          { key: 'linkedEditing', label: 'Linked Editing', desc: 'Simultaneously edit matching HTML tags.' },
        ].map(setting => (
          <Col span={12} key={setting.key}>
            <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', padding: '12px 16px', background: 'var(--studio-surface)', border: '1px solid var(--studio-border)', borderRadius: 8 }}>
              <div>
                <Text strong style={{ color: 'var(--studio-text)', display: 'block' }}>{setting.label}</Text>
                <Text style={{ color: 'var(--studio-muted)', fontSize: 12 }}>{setting.desc}</Text>
              </div>
              <Switch
                checked={editorSettings?.[setting.key]}
                onChange={v => updateEditorSettings({ [setting.key]: v })}
              />
            </div>
          </Col>
        ))}

        {/* Dropdowns */}
        <Col span={12}>
          <div style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
            <Text strong style={{ color: 'var(--studio-text)' }}>Word Wrap</Text>
            <Select
              value={editorSettings?.wordWrap || 'off'}
              onChange={v => updateEditorSettings({ wordWrap: v })}
              options={[
                { value: 'off', label: 'Off' },
                { value: 'on', label: 'On' },
                { value: 'wordWrapColumn', label: 'Word Wrap Column' },
                { value: 'bounded', label: 'Bounded' }
              ]}
            />
          </div>
        </Col>

        <Col span={12}>
          <div style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
            <Text strong style={{ color: 'var(--studio-text)' }}>Cursor Blinking</Text>
            <Select
              value={editorSettings?.cursorBlinking || 'smooth'}
              onChange={v => updateEditorSettings({ cursorBlinking: v })}
              options={[
                { value: 'blink', label: 'Blink' },
                { value: 'smooth', label: 'Smooth' },
                { value: 'phase', label: 'Phase' },
                { value: 'expand', label: 'Expand' },
                { value: 'solid', label: 'Solid' }
              ]}
            />
          </div>
        </Col>

        <Col span={12}>
          <div style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
            <Text strong style={{ color: 'var(--studio-text)' }}>Render Line Highlight</Text>
            <Select
              value={editorSettings?.renderLineHighlight || 'all'}
              onChange={v => updateEditorSettings({ renderLineHighlight: v })}
              options={[
                { value: 'none', label: 'None' },
                { value: 'gutter', label: 'Gutter' },
                { value: 'line', label: 'Line' },
                { value: 'all', label: 'All' }
              ]}
            />
          </div>
        </Col>

        <Col span={12}>
          <div style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
            <Text strong style={{ color: 'var(--studio-text)' }}>Render Whitespace</Text>
            <Select
              value={editorSettings?.renderWhitespace || 'none'}
              onChange={v => updateEditorSettings({ renderWhitespace: v })}
              options={[
                { value: 'none', label: 'None' },
                { value: 'boundary', label: 'Boundary' },
                { value: 'selection', label: 'Selection' },
                { value: 'trailing', label: 'Trailing' },
                { value: 'all', label: 'All' }
              ]}
            />
          </div>
        </Col>

        <Col span={12}>
          <div style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
            <Text strong style={{ color: 'var(--studio-text)' }}>Auto Closing Brackets</Text>
            <Select
              value={editorSettings?.autoClosingBrackets || 'always'}
              onChange={v => updateEditorSettings({ autoClosingBrackets: v })}
              options={[
                { value: 'always', label: 'Always' },
                { value: 'languageDefined', label: 'Language Defined' },
                { value: 'beforeWhitespace', label: 'Before Whitespace' },
                { value: 'never', label: 'Never' }
              ]}
            />
          </div>
        </Col>

        <Col span={12}>
          <div style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
            <Text strong style={{ color: 'var(--studio-text)' }}>Match Brackets</Text>
            <Select
              value={editorSettings?.matchBrackets || 'always'}
              onChange={v => updateEditorSettings({ matchBrackets: v })}
              options={[
                { value: 'always', label: 'Always' },
                { value: 'near', label: 'Near' },
                { value: 'never', label: 'Never' }
              ]}
            />
          </div>
        </Col>

      </Row>
    </div>
  );

  const agentContent = (
    <div style={{ marginTop: 24 }}>
      <Title level={4} style={{ color: 'var(--studio-text)', marginBottom: 8 }}>
        <ThunderboltOutlined style={{ marginRight: 8 }} /> Agent Behavior
      </Title>
      <Text style={{ color: 'var(--studio-muted)', display: 'block', marginBottom: 24 }}>
        Tune how the local AI agent reasons before responding.
      </Text>

      <Row gutter={[32, 24]}>
        <Col span={24}>
          <div style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
            <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
              <Text strong style={{ color: 'var(--studio-text)' }}>Max Thinking Tokens</Text>
              <InputNumber
                min={100} max={20000} step={100}
                value={agentSettings?.maxThinkingTokens || 1200}
                onChange={v => updateAgentSettings({ maxThinkingTokens: v })}
                style={{ width: 110 }}
              />
            </div>
            <Text style={{ color: 'var(--studio-muted)', fontSize: 12, marginBottom: 8 }}>
              How long the model may reason before it's cut off and nudged to act. Lower it for snappier but less thorough
              answers; raise it for more thorough reasoning on hard requests.
            </Text>
            <Slider
              min={100} max={20000} step={100}
              value={agentSettings?.maxThinkingTokens || 1200}
              onChange={v => updateAgentSettings({ maxThinkingTokens: v })}
              marks={{ 100: 'Fast', 1200: 'Default', 20000: 'Thorough' }}
            />
          </div>
        </Col>
        <Col span={24}>
          <div style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
            <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
              <Text strong style={{ color: 'var(--studio-text)' }}>Response Creativity</Text>
              <InputNumber
                min={0} max={1} step={0.1}
                value={agentSettings?.temperature ?? 0.7}
                onChange={v => updateAgentSettings({ temperature: v })}
                style={{ width: 110 }}
              />
            </div>
            <Text style={{ color: 'var(--studio-muted)', fontSize: 12, marginBottom: 8 }}>
              Controls how much the model varies its wording and choices between runs (this is what's usually called
              "temperature"). Lower it toward Precise for consistent, repeatable answers on the same prompt; raise it
              toward Creative for more varied phrasing and less predictable output.
            </Text>
            <Slider
              min={0} max={1} step={0.1}
              value={agentSettings?.temperature ?? 0.7}
              onChange={v => updateAgentSettings({ temperature: v })}
              marks={{ 0: 'Precise', 0.7: 'Default', 1: 'Creative' }}
            />
          </div>
        </Col>
        <Col span={24}>
          <div style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
            <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
              <Text strong style={{ color: 'var(--studio-text)' }}>Force-Unload Model Every N Chats</Text>
              <InputNumber
                min={0} max={2000} step={10}
                value={agentSettings?.forceUnloadAfterChats ?? 100}
                onChange={v => updateAgentSettings({ forceUnloadAfterChats: v })}
                style={{ width: 110 }}
              />
            </div>
            <Text style={{ color: 'var(--studio-muted)', fontSize: 12 }}>
              Periodically frees the model from memory after this many chat requests, even during continuous back-to-back use where the
              normal idle timeout never gets a chance to fire. Set to 0 to disable and rely on the idle timeout alone.
            </Text>
          </div>
        </Col>
      </Row>
    </div>
  );

  const tabItems = [
    { key: 'appearance', label: 'Appearance', children: appearanceContent },
    { key: 'typography', label: 'Typography', children: typographyContent },
    { key: 'editor', label: 'Editor', children: editorContent },
    { key: 'agent', label: 'Agent', children: agentContent }
  ];

  return (
    <div style={{ padding: '32px 48px', height: '100%', overflowY: 'auto', backgroundColor: 'var(--studio-bg, #f7f4ed)' }}>
      <div style={{ maxWidth: 900, margin: '0 auto' }}>
        <div style={{ marginBottom: 16 }}>
          <Title level={2} style={{ color: 'var(--studio-text)', margin: 0 }}>
            <FormatPainterOutlined style={{ marginRight: 12, color: 'var(--studio-accent)' }} />
            Settings
          </Title>
          <Text style={{ color: 'var(--studio-muted)' }}>Customize the look and feel of NativeStudio.</Text>
        </div>

        <Tabs defaultActiveKey="appearance" type="card" items={tabItems} className="pill-tabs" />

      </div>
    </div>
  );
}
