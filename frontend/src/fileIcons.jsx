import React from 'react';
import {
  SiJavascript, SiReact, SiTypescript, SiJson, SiHtml5, SiCss, SiSass, SiLess,
  SiMarkdown, SiGo, SiPhp, SiPython, SiRuby, SiRust, SiOpenjdk, SiC, SiCplusplus,
  SiYaml, SiToml, SiVuedotjs, SiSvelte, SiDocker, SiGit, SiDotenv, SiGnubash,
  SiGraphql, SiSvg, SiSqlite, SiKotlin, SiSwift, SiDart, SiComposer, SiNpm,
} from 'react-icons/si';
import { VscFile, VscFileMedia, VscFilePdf, VscFileZip, VscFileBinary, VscLock } from 'react-icons/vsc';

// Extension → { Icon, color }. Colors are each icon's real brand color where
// one exists (matching how VS Code icon themes color file icons), so
// languages stay visually distinct at a glance rather than all sharing one
// generic glyph.
const EXTENSION_ICONS = {
  js: [SiJavascript, '#f7df1e'], mjs: [SiJavascript, '#f7df1e'], cjs: [SiJavascript, '#f7df1e'],
  jsx: [SiReact, '#61dafb'], tsx: [SiReact, '#61dafb'],
  ts: [SiTypescript, '#3178c6'], mts: [SiTypescript, '#3178c6'], cts: [SiTypescript, '#3178c6'],
  json: [SiJson, '#a3a326'], jsonc: [SiJson, '#a3a326'],
  html: [SiHtml5, '#e34f26'], htm: [SiHtml5, '#e34f26'],
  css: [SiCss, '#264de4'],
  scss: [SiSass, '#cc6699'], sass: [SiSass, '#cc6699'],
  less: [SiLess, '#1d365d'],
  md: [SiMarkdown, '#5a6570'], mdx: [SiMarkdown, '#5a6570'],
  go: [SiGo, '#00add8'], mod: [SiGo, '#00add8'], sum: [SiGo, '#00add8'],
  php: [SiPhp, '#777bb4'],
  py: [SiPython, '#3776ab'], pyc: [SiPython, '#3776ab'],
  rb: [SiRuby, '#cc342d'],
  rs: [SiRust, '#dea584'],
  java: [SiOpenjdk, '#437291'], class: [SiOpenjdk, '#437291'],
  kt: [SiKotlin, '#7f52ff'], kts: [SiKotlin, '#7f52ff'],
  swift: [SiSwift, '#f05138'],
  dart: [SiDart, '#0175c2'],
  c: [SiC, '#a8b9cc'], h: [SiC, '#a8b9cc'],
  cpp: [SiCplusplus, '#00599c'], cc: [SiCplusplus, '#00599c'], cxx: [SiCplusplus, '#00599c'], hpp: [SiCplusplus, '#00599c'],
  yaml: [SiYaml, '#cb171e'], yml: [SiYaml, '#cb171e'],
  toml: [SiToml, '#9c4221'],
  vue: [SiVuedotjs, '#4fc08d'],
  svelte: [SiSvelte, '#ff3e00'],
  sh: [SiGnubash, '#4eaa25'], bash: [SiGnubash, '#4eaa25'], zsh: [SiGnubash, '#4eaa25'],
  graphql: [SiGraphql, '#e10098'], gql: [SiGraphql, '#e10098'],
  svg: [SiSvg, '#ffb13b'],
  sql: [SiSqlite, '#003b57'], sqlite: [SiSqlite, '#003b57'], db: [SiSqlite, '#003b57'],
  xml: [SiSvg, '#775ea8'],
  lock: [VscLock, '#8a939f'],
};

// Filenames matched exactly (case-insensitive), for files with no useful
// extension of their own — Dockerfile, dotfiles, manifests.
const FILENAME_ICONS = {
  dockerfile: [SiDocker, '#2496ed'],
  '.dockerignore': [SiDocker, '#2496ed'],
  '.gitignore': [SiGit, '#f05032'],
  '.gitattributes': [SiGit, '#f05032'],
  '.env': [SiDotenv, '#ecd53f'],
  'composer.json': [SiComposer, '#885630'],
  'composer.lock': [SiComposer, '#885630'],
  'package.json': [SiNpm, '#cb3837'],
  'package-lock.json': [SiNpm, '#cb3837'],
};

const IMAGE_EXTENSIONS = new Set(['png', 'jpg', 'jpeg', 'gif', 'webp', 'bmp', 'ico', 'avif']);
const ARCHIVE_EXTENSIONS = new Set(['zip', 'tar', 'gz', 'rar', '7z', 'bz2']);
const BINARY_EXTENSIONS = new Set(['exe', 'dll', 'so', 'dylib', 'bin', 'wasm']);

function resolveIcon(filename) {
  const lower = filename.toLowerCase();
  if (FILENAME_ICONS[lower]) return FILENAME_ICONS[lower];

  const dot = lower.lastIndexOf('.');
  const ext = dot > 0 ? lower.slice(dot + 1) : '';
  if (EXTENSION_ICONS[ext]) return EXTENSION_ICONS[ext];
  if (ext === 'pdf') return [VscFilePdf, '#dc3545'];
  if (IMAGE_EXTENSIONS.has(ext)) return [VscFileMedia, '#a074c4'];
  if (ARCHIVE_EXTENSIONS.has(ext)) return [VscFileZip, '#8a939f'];
  if (BINARY_EXTENSIONS.has(ext)) return [VscFileBinary, '#8a939f'];
  return [VscFile, '#8a939f'];
}

/** A per-language file icon, colored like a VS Code icon theme. */
export function FileTypeIcon({ filename = '', size = 16 }) {
  const [Icon, color] = resolveIcon(filename);
  return <Icon aria-hidden="true" size={size} color={color} style={{ flexShrink: 0 }} />;
}
