const uiFont = 'Inter, ui-sans-serif, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif';
const sfFont = '-apple-system, BlinkMacSystemFont, "SF Pro Text", "SF Pro Display", "Helvetica Neue", sans-serif';
const segoeFont = '"Segoe UI Variable", "Segoe UI", "Segoe UI Historic", -apple-system, sans-serif';

const makeTheme = (id, name, pairId, dark, layout, colors, options = {}) => ({
  id, name, pairId, dark, layout,
  font: options.font || uiFont,
  radius: options.radius ?? 10,
  ...colors,
});

export const studioThemes = [
  // 0. Default NativeStudio
  makeTheme('default', 'Default', 'default', false, 'standard', { bg: '#f7f4ed', surface: '#fffdf8', panel: '#eee9df', border: '#d8d1c5', text: '#2f2a26', muted: '#746b63', subtle: '#91877e', accent: '#c15f3c', accentSoft: '#f3dfd5', success: '#6b8f71', danger: '#b85c5c', warning: '#c69455' }, { radius: 8 }),
  makeTheme('default-dark', 'Default Dark', 'default', true, 'standard', { bg: '#1c1917', surface: '#292524', panel: '#44403c', border: '#57534e', text: '#fafaf9', muted: '#a8a29e', subtle: '#d6d3d1', accent: '#fb923c', accentSoft: '#7c2d12', success: '#86ab8f', danger: '#d98686', warning: '#d4ab7a' }, { radius: 8 }),

  // 1. Dribbble Bento
  makeTheme('bento-light', 'Bento Light', 'bento', false, 'bento', { bg: '#F3F4F6', surface: '#FFFFFF', panel: '#FFFFFF', border: '#E5E7EB', text: '#111827', muted: '#6B7280', subtle: '#9CA3AF', accent: '#8B5CF6', accentSoft: '#EDE9FE', success: '#22C55E', danger: '#EF4444', warning: '#F59E0B' }, { radius: 16 }),
  makeTheme('bento-dark', 'Bento Dark', 'bento', true, 'bento', { bg: '#0F172A', surface: '#1E293B', panel: '#1E293B', border: '#334155', text: '#F8FAFC', muted: '#94A3B8', subtle: '#64748B', accent: '#A78BFA', accentSoft: '#2E1065', success: '#4ADE80', danger: '#F87171', warning: '#FBBF24' }, { radius: 16 }),

  // 2. Glassmorphism
  makeTheme('glass-light', 'Glass Light', 'glass', false, 'glass', { bg: '#FDF2F8', surface: 'rgba(255,255,255,0.7)', panel: 'rgba(255,255,255,0.5)', border: 'rgba(255,255,255,0.3)', text: '#4A044E', muted: '#9D174D', subtle: '#BE185D', accent: '#DB2777', accentSoft: 'rgba(219,39,119,0.1)', success: '#10B981', danger: '#DC2626', warning: '#D97706' }, { radius: 24 }),
  makeTheme('glass-dark', 'Glass Dark', 'glass', true, 'glass', { bg: '#0A0F24', surface: 'rgba(16,25,48,0.7)', panel: 'rgba(16,25,48,0.5)', border: 'rgba(255,255,255,0.1)', text: '#E2E8F0', muted: '#94A3B8', subtle: '#64748B', accent: '#38BDF8', accentSoft: 'rgba(56,189,248,0.1)', success: '#34D399', danger: '#F87171', warning: '#FBBF24' }, { radius: 24 }),

  // 3. Google Chrome
  makeTheme('chrome-light', 'Chrome Light', 'chrome', false, 'chrome', { bg: '#F1F3F4', surface: '#FFFFFF', panel: '#F1F3F4', border: '#DADCE0', text: '#202124', muted: '#5F6368', subtle: '#80868B', accent: '#1A73E8', accentSoft: '#D2E3FC', success: '#188038', danger: '#D93025', warning: '#F9AB00' }, { radius: 8 }),
  makeTheme('chrome-dark', 'Chrome Dark', 'chrome', true, 'chrome', { bg: '#202124', surface: '#292A2D', panel: '#292A2D', border: '#3C4043', text: '#E8EAED', muted: '#9AA0A6', subtle: '#5F6368', accent: '#8AB4F8', accentSoft: '#174EA6', success: '#81C995', danger: '#F28B82', warning: '#FDD663' }, { radius: 8 }),

  // 4. YouTube
  makeTheme('youtube-light', 'YouTube Light', 'youtube', false, 'youtube', { bg: '#F9F9F9', surface: '#FFFFFF', panel: '#F9F9F9', border: '#E5E5E5', text: '#0F0F0F', muted: '#606060', subtle: '#909090', accent: '#FF0000', accentSoft: '#FFE0E0', success: '#2BA640', danger: '#CC0000', warning: '#AA5500' }, { radius: 12 }),
  makeTheme('youtube-dark', 'YouTube Dark', 'youtube', true, 'youtube', { bg: '#0F0F0F', surface: '#0F0F0F', panel: '#272727', border: '#3F3F3F', text: '#F1F1F1', muted: '#AAAAAA', subtle: '#717171', accent: '#FF0000', accentSoft: '#330000', success: '#3DB350', danger: '#F28B82', warning: '#FFD500' }, { radius: 12 }),

  // 5. Netflix
  makeTheme('netflix-light', 'Netflix Light', 'netflix', false, 'netflix', { bg: '#FFFFFF', surface: '#F3F3F3', panel: '#FFFFFF', border: '#E6E6E6', text: '#333333', muted: '#808080', subtle: '#B3B3B3', accent: '#E50914', accentSoft: '#FCE7E8', success: '#2E7D32', danger: '#B00020', warning: '#B26A00' }, { radius: 4 }),
  makeTheme('netflix-dark', 'Netflix Dark', 'netflix', true, 'netflix', { bg: '#141414', surface: '#141414', panel: '#1F1F1F', border: '#333333', text: '#FFFFFF', muted: '#B3B3B3', subtle: '#808080', accent: '#E50914', accentSoft: '#3D0A0D', success: '#46D369', danger: '#FF6B5E', warning: '#FFC145' }, { radius: 4 }),

  // 6. macOS Glass — real macOS vibrancy (edge-to-edge frosted panels, hairline
  // dividers, SF font, system blue), not the colorful pink/blue "Glass" theme
  // above which is a generic glassmorphism look rather than a macOS one.
  makeTheme('macos-light', 'macOS Light', 'macos', false, 'macos', { bg: '#F5F5F7', surface: 'rgba(255,255,255,0.72)', panel: 'rgba(246,246,246,0.6)', border: 'rgba(0,0,0,0.08)', text: '#1D1D1F', muted: '#6E6E73', subtle: '#86868B', accent: '#007AFF', accentSoft: 'rgba(0,122,255,0.12)', success: '#34C759', danger: '#FF3B30', warning: '#FF9500' }, { font: sfFont, radius: 12 }),
  makeTheme('macos-dark', 'macOS Dark', 'macos', true, 'macos', { bg: '#1E1E1E', surface: 'rgba(30,30,30,0.72)', panel: 'rgba(40,40,40,0.6)', border: 'rgba(255,255,255,0.09)', text: '#F5F5F7', muted: '#98989D', subtle: '#6E6E73', accent: '#0A84FF', accentSoft: 'rgba(10,132,255,0.16)', success: '#32D74B', danger: '#FF453A', warning: '#FF9F0A' }, { font: sfFont, radius: 12 }),

  // 7. Windows 11 — Fluent/Mica: flat flush panels (no floating cards), thin
  // dividers, Segoe UI Variable, the 8px corner radius Windows 11 actually uses.
  makeTheme('win11-light', 'Windows 11 Light', 'win11', false, 'win11', { bg: '#F3F3F3', surface: '#FFFFFF', panel: '#F9F9F9', border: '#E5E5E5', text: '#1A1A1A', muted: '#5D5D5D', subtle: '#8A8A8A', accent: '#0078D4', accentSoft: '#DEECF9', success: '#107C10', danger: '#C42B1C', warning: '#9D5D00' }, { font: segoeFont, radius: 8 }),
  makeTheme('win11-dark', 'Windows 11 Dark', 'win11', true, 'win11', { bg: '#202020', surface: '#2C2C2C', panel: '#272727', border: '#3B3B3B', text: '#FFFFFF', muted: '#C5C5C5', subtle: '#9C9C9C', accent: '#60CDFF', accentSoft: '#173344', success: '#6CCB5F', danger: '#FF99A4', warning: '#FFB900' }, { font: segoeFont, radius: 8 }),

  // 8. Windows 12 (concept) — Windows 12 isn't an actual released Microsoft
  // product; this follows the common community-concept direction (more
  // rounded than Win11, heavier Mica/glass, more vibrant accent) rather than
  // any confirmed spec.
  makeTheme('win12-light', 'Windows 12 Light', 'win12', false, 'win12', { bg: '#EFF3FB', surface: 'rgba(255,255,255,0.78)', panel: 'rgba(255,255,255,0.55)', border: 'rgba(0,0,0,0.06)', text: '#141414', muted: '#5B6472', subtle: '#8891A0', accent: '#3E63DD', accentSoft: 'rgba(62,99,221,0.14)', success: '#1F9D55', danger: '#E5484D', warning: '#F5A524' }, { font: segoeFont, radius: 16 }),
  makeTheme('win12-dark', 'Windows 12 Dark', 'win12', true, 'win12', { bg: '#15161B', surface: 'rgba(24,26,32,0.75)', panel: 'rgba(30,32,40,0.6)', border: 'rgba(255,255,255,0.08)', text: '#F2F3F5', muted: '#A2A8B5', subtle: '#767D8C', accent: '#7B87F5', accentSoft: 'rgba(123,135,245,0.18)', success: '#3DD68C', danger: '#FF6B6B', warning: '#FFB454' }, { font: segoeFont, radius: 16 }),

  // 9. Liquid Glass — Apple's current design language (introduced WWDC June
  // 2025, mandatory across iOS/iPadOS/macOS by Sept 2026): variable-opacity
  // (20-80%) refractive glass panels that float above content with rounded,
  // hardware-matched geometry, a moving specular highlight, and controls
  // that read as glass rather than flat fills — a real, current Apple
  // design system, not the 2000s "Aqua" look.
  makeTheme('liquidglass-light', 'Liquid Glass Light', 'liquidglass', false, 'liquidglass', { bg: '#EDEDF2', surface: 'rgba(255,255,255,0.55)', panel: 'rgba(255,255,255,0.4)', border: 'rgba(255,255,255,0.6)', text: '#1C1C1E', muted: '#6E6E73', subtle: '#8E8E93', accent: '#007AFF', accentSoft: 'rgba(0,122,255,0.16)', success: '#30D158', danger: '#FF453A', warning: '#FF9F0A' }, { font: sfFont, radius: 22 }),
  makeTheme('liquidglass-dark', 'Liquid Glass Dark', 'liquidglass', true, 'liquidglass', { bg: '#0B0B10', surface: 'rgba(30,30,36,0.55)', panel: 'rgba(28,28,34,0.42)', border: 'rgba(255,255,255,0.14)', text: '#F2F2F7', muted: '#98989D', subtle: '#6E6E73', accent: '#0A84FF', accentSoft: 'rgba(10,132,255,0.22)', success: '#30D158', danger: '#FF453A', warning: '#FFD60A' }, { font: sfFont, radius: 22 }),
];

export const getStudioTheme = id => studioThemes.find(theme => theme.id === id) || studioThemes[0];

export const themeVariables = theme => ({
  '--studio-bg': theme.bg,
  '--studio-surface': theme.surface,
  '--studio-panel': theme.panel,
  '--studio-border': theme.border,
  '--studio-text': theme.text,
  '--studio-muted': theme.muted,
  '--studio-subtle': theme.subtle,
  '--studio-accent': theme.accent,
  '--studio-accent-soft': theme.accentSoft,
  '--studio-success': theme.success,
  '--studio-danger': theme.danger,
  '--studio-warning': theme.warning,
  '--studio-radius': `${theme.radius}px`,
  '--studio-font': theme.font,
});
