export const FONT_FAMILIES = [
  { id: 'system', label: 'System Default', value: 'ui-monospace, SFMono-Regular, Menlo, Consolas, monospace', ligatures: false },
  { id: 'menlo', label: 'Menlo', value: 'Menlo, Consolas, monospace', ligatures: false },
  { id: 'sfmono', label: 'SF Mono', value: '"SF Mono", Menlo, Consolas, monospace', ligatures: false },
  { id: 'consolas', label: 'Consolas', value: 'Consolas, Menlo, monospace', ligatures: false },
  { id: 'cascadia', label: 'Cascadia Code', value: '"Cascadia Code", Menlo, Consolas, monospace', ligatures: true },
  { id: 'fira', label: 'Fira Code', value: '"Fira Code", Menlo, Consolas, monospace', ligatures: true },
  { id: 'jetbrains', label: 'JetBrains Mono', value: '"JetBrains Mono", Menlo, Consolas, monospace', ligatures: true },
  { id: 'sourcecodepro', label: 'Source Code Pro', value: '"Source Code Pro", Menlo, Consolas, monospace', ligatures: false },
  { id: 'ibmplex', label: 'IBM Plex Mono', value: '"IBM Plex Mono", Menlo, Consolas, monospace', ligatures: false },
  { id: 'robotomono', label: 'Roboto Mono', value: '"Roboto Mono", Menlo, Consolas, monospace', ligatures: false },
];

export const DEFAULT_FONT_SETTINGS = {
  fontFamilyId: 'system',
  fontSize: 14,
  lineHeight: 1.5,
  letterSpacing: 0,
  ligatures: false,
};

const STORAGE_KEY = 'nativestudio.fontSettings';

export function loadFontSettings() {
  try {
    const raw = window.localStorage.getItem(STORAGE_KEY);
    if (!raw) return DEFAULT_FONT_SETTINGS;
    return { ...DEFAULT_FONT_SETTINGS, ...JSON.parse(raw) };
  } catch {
    return DEFAULT_FONT_SETTINGS;
  }
}

export function saveFontSettings(settings) {
  window.localStorage.setItem(STORAGE_KEY, JSON.stringify(settings));
}

export function fontFamilyValue(id) {
  return (FONT_FAMILIES.find(f => f.id === id) || FONT_FAMILIES[0]).value;
}
