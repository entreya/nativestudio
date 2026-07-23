// Builds/parses the "/projects/:id/files/*" URL shape used to keep the
// currently open file (and cursor position) reflected in the address bar,
// so a deep link or browser back/forward restores exactly what was open.

export function fileURL(projectId, path, cursor) {
  const segments = path.split('/').filter(Boolean).map(encodeURIComponent).join('/');
  const query = cursor && (cursor.line || cursor.column)
    ? `?line=${cursor.line || 1}&col=${cursor.column || 1}`
    : '';
  return `/projects/${projectId}/files/${segments}${query}`;
}

export function splatToFilePath(splat) {
  if (!splat) return '';
  return '/' + splat.split('/').filter(Boolean).map(decodeURIComponent).join('/');
}
