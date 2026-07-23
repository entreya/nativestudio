const uiFont = 'Inter, ui-sans-serif, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif';
const monoFont = '"SFMono-Regular", "Cascadia Code", Menlo, Consolas, monospace';

const makeTheme = (id, name, description, dark, layout, colors, options = {}) => ({
  id, name, description, dark, layout,
  font: options.font || uiFont,
  radius: options.radius ?? 10,
  ...colors,
});

// Presets modeled after well-known product interfaces — not just recolors:
// the `layout` field selects one of the studio-layout-* CSS variants (see
// index.css), so panel density, corner radius, borders, and blur/shadow
// treatment change with the theme too, not only its colors.
export const studioThemes = [
  makeTheme('default', 'Default', 'The original warm NativeStudio appearance and standard editor layout.', false, 'standard', { bg: '#f7f4ed', surface: '#fffdf8', panel: '#eee9df', border: '#d8d1c5', text: '#2f2a26', muted: '#746b63', subtle: '#91877e', accent: '#c15f3c', accentSoft: '#f3dfd5' }, { radius: 8 }),
  makeTheme('linear', 'Linear', 'Near-black canvas, dense panels, and Linear\'s signature indigo.', true, 'compact', { bg: '#08090a', surface: '#101113', panel: '#18191c', border: '#26282c', text: '#f7f8f8', muted: '#8a8f98', subtle: '#5c6066', accent: '#5e6ad2', accentSoft: '#23253f' }, { radius: 6 }),
  makeTheme('stripe', 'Stripe', 'Soft blurple gradients over crisp white, Stripe dashboard style.', false, 'glass', { bg: '#f6f9fc', surface: '#ffffff', panel: '#eef1f8', border: '#d8dee8', text: '#1a1f36', muted: '#697386', subtle: '#8792a2', accent: '#635bff', accentSoft: '#e0defe' }, { radius: 14 }),
  makeTheme('github', 'GitHub Dark', 'The familiar dark GitHub canvas with its classic blue links.', true, 'standard', { bg: '#0d1117', surface: '#161b22', panel: '#21262d', border: '#30363d', text: '#c9d1d9', muted: '#8b949e', subtle: '#6e7681', accent: '#58a6ff', accentSoft: '#1f3a5f' }, { radius: 6 }),
  makeTheme('notion', 'Notion', 'Warm off-white paper with quiet, minimal chrome.', false, 'rounded', { bg: '#ffffff', surface: '#ffffff', panel: '#f7f6f3', border: '#e9e9e7', text: '#37352f', muted: '#9b9a97', subtle: '#b3b1ac', accent: '#2383e2', accentSoft: '#dbedff' }, { radius: 10 }),
  makeTheme('raycast', 'Raycast', 'Deep dark blur with Raycast\'s signature red-orange glow.', true, 'floating', { bg: '#0d0d0f', surface: '#161618', panel: '#1e1e21', border: '#2f2f34', text: '#ffffff', muted: '#a0a0a8', subtle: '#717179', accent: '#ff6363', accentSoft: '#4a1f22' }, { radius: 14 }),
  makeTheme('figma', 'Figma', 'Bright light canvas with playful geometry and Figma purple.', false, 'rounded', { bg: '#f5f5f5', surface: '#ffffff', panel: '#ececec', border: '#e0e0e0', text: '#0d0d0d', muted: '#6e6e6e', subtle: '#8f8f8f', accent: '#a259ff', accentSoft: '#ede3ff' }, { radius: 12 }),
  makeTheme('vercel', 'Vercel', 'Pure black-and-white terminal precision with an electric-blue signal.', true, 'terminal', { bg: '#000000', surface: '#0a0a0a', panel: '#171717', border: '#333333', text: '#ffffff', muted: '#a1a1a1', subtle: '#666666', accent: '#0070f3', accentSoft: '#001d3d' }, { font: monoFont, radius: 0 }),
  makeTheme('arc', 'Arc', 'Warm cream gradients with Arc\'s playful coral-pink accent.', false, 'floating', { bg: '#fdf6f0', surface: '#ffffff', panel: '#fbe9e7', border: '#f0d9d0', text: '#3d2b2b', muted: '#8a7570', subtle: '#ab938d', accent: '#ff6f9c', accentSoft: '#ffe0ec' }, { radius: 18 }),
  makeTheme('supabase', 'Supabase', 'Console-dark green-black with Supabase\'s bright signature green.', true, 'terminal', { bg: '#0a0a0a', surface: '#121212', panel: '#1c1c1c', border: '#2e2e2e', text: '#ededed', muted: '#a0a0a0', subtle: '#707070', accent: '#3ecf8e', accentSoft: '#123825' }, { font: monoFont, radius: 2 }),
  makeTheme('spotify', 'Spotify', 'Dense dark player chrome with Spotify\'s vivid green.', true, 'compact', { bg: '#121212', surface: '#181818', panel: '#202020', border: '#2a2a2a', text: '#ffffff', muted: '#b3b3b3', subtle: '#727272', accent: '#1db954', accentSoft: '#123822' }, { radius: 8 }),
  makeTheme('netflix', 'Netflix', 'Cinematic black with Netflix\'s unmistakable red.', true, 'terminal', { bg: '#000000', surface: '#141414', panel: '#1f1f1f', border: '#333333', text: '#ffffff', muted: '#b3b3b3', subtle: '#808080', accent: '#e50914', accentSoft: '#3d0a0d' }, { radius: 2 }),
  makeTheme('instagram', 'Instagram', 'Clean white feed with Instagram\'s warm gradient pink.', false, 'rounded', { bg: '#fafafa', surface: '#ffffff', panel: '#f0f0f0', border: '#dbdbdb', text: '#262626', muted: '#8e8e8e', subtle: '#a8a8a8', accent: '#e1306c', accentSoft: '#fde0ea' }, { radius: 12 }),
  makeTheme('youtube', 'YouTube', 'Bright neutral surfaces with YouTube\'s signature red.', false, 'standard', { bg: '#ffffff', surface: '#ffffff', panel: '#f2f2f2', border: '#e5e5e5', text: '#0f0f0f', muted: '#606060', subtle: '#909090', accent: '#ff0000', accentSoft: '#ffe0e0' }, { radius: 8 }),
  makeTheme('google', 'Google', 'Airy Material canvas with Google\'s four-color blue lead.', false, 'floating', { bg: '#f8f9fa', surface: '#ffffff', panel: '#f1f3f4', border: '#dadce0', text: '#202124', muted: '#5f6368', subtle: '#80868b', accent: '#1a73e8', accentSoft: '#d2e3fc' }, { radius: 16 }),
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
  '--studio-radius': `${theme.radius}px`,
  '--studio-font': theme.font,
});
