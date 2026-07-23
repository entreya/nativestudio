import React from 'react';
import { getFileColor } from './fileColors';

// Short label shown inside the file-type badge — kept to 2-4 characters so it
// stays legible at sidebar/tree row size.
const EXTENSION_LABELS = {
  js: 'JS', jsx: 'JSX', ts: 'TS', tsx: 'TSX', json: '{}', html: '<>', css: '#',
  md: 'MD', go: 'GO', mod: 'GO', sum: 'GO', php: 'PHP', py: 'PY', sql: 'SQL',
  yaml: 'YML', yml: 'YML', xml: 'XML', toml: 'TML',
};

/**
 * A small colored badge naming a file's language/type (e.g. "PHP", "GO",
 * "JS"), rather than one generic file icon for every extension. No icon font
 * or SVG set is in this project, so this renders as text-in-a-badge — still
 * gives each language a distinct, recognizable mark in the tree.
 */
export function FileTypeIcon({ filename = '', size = 15 }) {
  const dot = filename.lastIndexOf('.');
  const ext = dot >= 0 ? filename.slice(dot + 1).toLowerCase() : '';
  const label = EXTENSION_LABELS[ext] || (ext ? ext.slice(0, 3).toUpperCase() : '•');
  return (
    <span
      aria-hidden="true"
      style={{
        display: 'inline-flex',
        alignItems: 'center',
        justifyContent: 'center',
        minWidth: size,
        height: size,
        padding: '0 3px',
        borderRadius: 4,
        background: getFileColor(filename),
        color: '#fff',
        fontSize: Math.round(size * 0.6),
        fontWeight: 700,
        fontFamily: 'ui-monospace, SFMono-Regular, Menlo, monospace',
        lineHeight: 1,
        letterSpacing: '-0.03em',
        flexShrink: 0,
      }}
    >
      {label}
    </span>
  );
}
