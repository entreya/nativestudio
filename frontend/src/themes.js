const uiFont = 'Inter, ui-sans-serif, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif';
const editorialFont = '"Avenir Next", Inter, ui-sans-serif, sans-serif';
const monoFont = '"SFMono-Regular", "Cascadia Code", Menlo, Consolas, monospace';

const makeTheme = (id, name, description, dark, layout, colors, options = {}) => ({
  id, name, description, dark, layout,
  font: options.font || uiFont,
  radius: options.radius ?? 10,
  ...colors,
});

export const studioThemes = [
  makeTheme('default', 'Default', 'The original warm NativeStudio appearance and standard editor layout.', false, 'standard', { bg: '#f7f4ed', surface: '#fffdf8', panel: '#eee9df', border: '#d8d1c5', text: '#2f2a26', muted: '#746b63', subtle: '#91877e', accent: '#c15f3c', accentSoft: '#f3dfd5' }, { radius: 8 }),
  makeTheme('atelier', 'Atelier', 'Warm editorial focus with calm paper surfaces.', false, 'rounded', { bg: '#f7f4ed', surface: '#fffdf8', panel: '#eee9df', border: '#d8d1c5', text: '#2f2a26', muted: '#746b63', subtle: '#91877e', accent: '#c15f3c', accentSoft: '#f3dfd5' }, { font: editorialFont, radius: 10 }),
  makeTheme('polar', 'Polar Glass', 'Cool translucent layers with an arctic blue signal.', false, 'glass', { bg: '#eaf2f8', surface: '#f8fcff', panel: '#dceaf4', border: '#b9ccda', text: '#142638', muted: '#536b7d', subtle: '#7890a3', accent: '#2379c9', accentSoft: '#d5eaff' }, { radius: 16 }),
  makeTheme('mintwave', 'Mintwave', 'Fresh mint canvas with precise emerald controls.', false, 'floating', { bg: '#eaf8f2', surface: '#fbfffd', panel: '#d9f0e6', border: '#b7d9cb', text: '#15362c', muted: '#527568', subtle: '#779a8e', accent: '#16856a', accentSoft: '#c9eee3' }, { radius: 14 }),
  makeTheme('orchid', 'Orchid Air', 'Soft lilac workspace with expressive violet energy.', false, 'floating', { bg: '#f3effb', surface: '#fdfbff', panel: '#e8e0f5', border: '#cfc3df', text: '#302441', muted: '#746683', subtle: '#9588a4', accent: '#7651c9', accentSoft: '#e4d8ff' }, { radius: 16 }),
  makeTheme('sakura', 'Sakura Signal', 'Clean blush neutrals with a vivid coral pulse.', false, 'rounded', { bg: '#fff3f5', surface: '#fffafb', panel: '#f8e3e7', border: '#e5c3ca', text: '#41262d', muted: '#806069', subtle: '#a1848c', accent: '#d64f70', accentSoft: '#ffdbe4' }, { radius: 13 }),
  makeTheme('cobalt', 'Cobalt Studio', 'Crisp white architecture and saturated blue actions.', false, 'compact', { bg: '#edf3ff', surface: '#ffffff', panel: '#dfe9fb', border: '#bdcce7', text: '#18243a', muted: '#586985', subtle: '#7d8da7', accent: '#245fd6', accentSoft: '#d8e5ff' }, { radius: 8 }),
  makeTheme('sandbar', 'Sandbar', 'Sunlit mineral tones with modern terracotta details.', false, 'rounded', { bg: '#f5efe4', surface: '#fffaf0', panel: '#eae0cf', border: '#d2c2aa', text: '#382d20', muted: '#766752', subtle: '#978872', accent: '#b85f32', accentSoft: '#f4d7c5' }, { font: editorialFont, radius: 12 }),
  makeTheme('cloud', 'Cloud Console', 'Neutral, low-noise surfaces for long focused sessions.', false, 'compact', { bg: '#f2f4f7', surface: '#ffffff', panel: '#e7ebf0', border: '#cbd2dc', text: '#20262f', muted: '#646d79', subtle: '#8a939f', accent: '#5068d8', accentSoft: '#dfe4ff' }, { radius: 7 }),
  makeTheme('lime', 'Lime Circuit', 'Bright technical canvas with electric chartreuse cues.', false, 'terminal', { bg: '#f2f7e9', surface: '#fbfff5', panel: '#e3edcf', border: '#c4d3a7', text: '#202b18', muted: '#637151', subtle: '#879475', accent: '#5f8f16', accentSoft: '#dff3b9' }, { font: monoFont, radius: 2 }),
  makeTheme('peach', 'Peach Fuzz', 'Optimistic peach atmosphere with confident orange focus.', false, 'glass', { bg: '#fff0e7', surface: '#fffaf6', panel: '#f8dfd0', border: '#e7c1ad', text: '#402a21', muted: '#826455', subtle: '#a08272', accent: '#dc6335', accentSoft: '#ffd9c6' }, { radius: 18 }),
  makeTheme('midnight', 'Midnight Flux', 'Deep navy layers with luminous cyan navigation.', true, 'floating', { bg: '#08111f', surface: '#0f1b2d', panel: '#15243a', border: '#263a53', text: '#e5efff', muted: '#94a8c2', subtle: '#6f849e', accent: '#42b9ff', accentSoft: '#123b57' }, { radius: 12 }),
  makeTheme('graphite', 'Graphite Pro', 'Dense charcoal workbench with restrained indigo.', true, 'compact', { bg: '#111318', surface: '#181b21', panel: '#20242c', border: '#303640', text: '#eceef2', muted: '#a3a8b2', subtle: '#777e89', accent: '#8094ff', accentSoft: '#293050' }, { radius: 7 }),
  makeTheme('aurora', 'Aurora Deck', 'Northern-light gradients over crisp dark glass.', true, 'glass', { bg: '#071816', surface: '#0d2422', panel: '#12312e', border: '#28504b', text: '#e0fff9', muted: '#91bbb3', subtle: '#6d9991', accent: '#39e0b4', accentSoft: '#164c41' }, { radius: 17 }),
  makeTheme('neon', 'Neon Noir', 'Near-black canvas with a sharp magenta interface glow.', true, 'terminal', { bg: '#09080d', surface: '#121017', panel: '#1b1721', border: '#3b2b43', text: '#f8ebff', muted: '#b09bb9', subtle: '#806e89', accent: '#ed55d8', accentSoft: '#4a1d45' }, { font: monoFont, radius: 2 }),
  makeTheme('ember', 'Ember Core', 'Smoked surfaces energized by molten orange accents.', true, 'floating', { bg: '#17100d', surface: '#211713', panel: '#2b1e18', border: '#4b3228', text: '#fff0e8', muted: '#c0a093', subtle: '#92776d', accent: '#ff7b42', accentSoft: '#542b1e' }, { radius: 12 }),
  makeTheme('ultraviolet', 'Ultraviolet', 'Inky purple depth with bright spectral highlights.', true, 'rounded', { bg: '#100c1b', surface: '#181226', panel: '#211933', border: '#382b51', text: '#f1eaff', muted: '#afa0c8', subtle: '#82749a', accent: '#a980ff', accentSoft: '#35245e' }, { radius: 15 }),
  makeTheme('deepsea', 'Deep Sea', 'Oceanic blue-black surfaces and bioluminescent aqua.', true, 'glass', { bg: '#06131a', surface: '#0b2029', panel: '#102c37', border: '#254754', text: '#e4faff', muted: '#8db7c1', subtle: '#668f99', accent: '#31c5d8', accentSoft: '#124652' }, { radius: 14 }),
  makeTheme('forest', 'Forest Night', 'Quiet evergreen layers with a crisp lime beacon.', true, 'rounded', { bg: '#0b1510', surface: '#122019', panel: '#192b21', border: '#2e4938', text: '#e8f7ed', muted: '#9bb5a4', subtle: '#718e7b', accent: '#73d68c', accentSoft: '#244b30' }, { radius: 11 }),
  makeTheme('carbon', 'Carbon Redline', 'High-performance black with focused red telemetry.', true, 'compact', { bg: '#0d0f12', surface: '#15181c', panel: '#1d2127', border: '#343941', text: '#f4f5f6', muted: '#a3a7ad', subtle: '#737981', accent: '#f05252', accentSoft: '#482124' }, { radius: 6 }),
  makeTheme('horizon', 'Horizon Dusk', 'Blue-hour surfaces with a warm sunset focal color.', true, 'floating', { bg: '#121522', surface: '#1a1e2d', panel: '#22283a', border: '#353e57', text: '#f2f1ff', muted: '#aaa9c3', subtle: '#7d7e99', accent: '#ff8d76', accentSoft: '#543036' }, { radius: 13 }),
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
