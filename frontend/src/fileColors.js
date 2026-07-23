// Shared by fileIcons.jsx (and anything else that wants a color without the
// badge component) so file-type coloring stays consistent across the app.
// Kept in a plain .js module, separate from fileIcons.jsx's component
// export, so Vite's fast-refresh boundary stays clean (mixing a component
// export with plain function/constant exports in one file breaks it).
export const getFileColor = (filename = '') => {
  if (filename.endsWith('.js') || filename.endsWith('.jsx')) return '#ca8a04';
  if (filename.endsWith('.ts') || filename.endsWith('.tsx')) return '#2563eb';
  if (filename.endsWith('.json')) return '#16a34a';
  if (filename.endsWith('.html')) return '#ea580c';
  if (filename.endsWith('.css')) return '#0284c7';
  if (filename.endsWith('.md')) return '#64748b';
  if (filename.endsWith('.go') || filename.endsWith('.mod') || filename.endsWith('.sum')) return '#0891b2';
  if (filename.endsWith('.php')) return '#7c3aed';
  if (filename.endsWith('.py')) return '#ca8a04';
  if (filename.endsWith('.sql')) return '#db2777';
  if (filename.endsWith('.yaml') || filename.endsWith('.yml')) return '#0d9488';
  if (filename.endsWith('.xml') || filename.endsWith('.toml')) return '#9333ea';
  return '#94a3b8';
};
