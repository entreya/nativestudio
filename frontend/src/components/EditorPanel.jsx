import React, { useState, useEffect, useRef, useMemo } from 'react';
import Editor from '@monaco-editor/react';
import { Typography, theme, Skeleton } from 'antd';
import { fontFamilyValue, DEFAULT_FONT_SETTINGS } from '../state/fontSettings';

const { Text } = Typography;
const { useToken } = theme;


/**
 * EditorPanel renders a Monaco editor for the given file path.
 *
 * Props:
 *  - filepath: string — path of the currently active file
 *  - onCursorChange(line, col): called on cursor position changes
 *  - onSelectionChange(startLine, endLine, text): called on selection changes
 *  - onSymbolChange(symbol | null): called when word-at-cursor changes
 *  - onEdit(file, startLine, endLine): called on model content changes
 *  - onFileOpen(): currently unused placeholder for future tab sync
 *  - initialCursor: { line, column } — where to place the cursor once this
 *    file's content has loaded (e.g. restoring position from a deep link)
 *  - fontSettings: { fontFamilyId, fontSize, lineHeight, letterSpacing, ligatures }
 */
export default function EditorPanel({
  filepath,
  aiPreview,
  darkMode = false,
  onCursorChange,
  onSelectionChange,
  onSymbolChange,
  onEdit,
  initialCursor,
  fontSettings = DEFAULT_FONT_SETTINGS,
  editorSettings = {},
}) {
  const [content, setContent] = useState('');
  const [unsaved, setUnsaved] = useState(false);
  const [loadError, setLoadError] = useState('');
  const [isLoading, setIsLoading] = useState(false);
  const editorRef = useRef(null);
  const { token } = useToken();
  // Track the previous word to avoid firing onSymbolChange for unchanged words.
  const lastWordRef = useRef('');

  // window.editor/getCurrentContent/getSelectedText are set in handleEditorMount
  // for ChatPanel's diff logic to reach into. They get reassigned whenever a
  // new file mounts a fresh Monaco instance, but nothing clears them when this
  // panel unmounts entirely (e.g. the last tab closes) — without this, later
  // calls would operate on a disposed editor instead of failing cleanly.
  useEffect(() => {
    return () => {
      window.editor = null;
      window.getCurrentContent = null;
      window.getSelectedText = null;
    };
  }, []);

  useEffect(() => {
    window.currentFile = filepath || '';
    window.reloadCurrentFile = async () => {
      if (!window.currentFile) return;
      const response = await fetch(`/api/file?path=${encodeURIComponent(window.currentFile)}`);
      if (!response.ok) throw new Error((await response.text()) || 'Could not reload file');
      const data = await response.json();
      const nextContent = data.content ?? '';
      setContent(nextContent);
      editorRef.current?.setValue(nextContent);
      setUnsaved(false);
    };
    return () => {
      if (window.currentFile === filepath) window.currentFile = '';
    };
  }, [filepath]);

  useEffect(() => {
    if (filepath) {
      let cancelled = false;
      setIsLoading(true);
      setContent('');
      setLoadError('');

      fetch(`/api/file?path=${encodeURIComponent(filepath)}`)
        .then(async res => {
          if (!res.ok) throw new Error((await res.text()) || `Could not read file (${res.status})`);
          return res.json();
        })
        .then(data => {
          if (cancelled) return;
          const hasPreview = aiPreview?.path === filepath && typeof aiPreview.suggested === 'string';
          setContent(hasPreview ? aiPreview.suggested : (data.content ?? ''));
          setUnsaved(hasPreview);
          setLoadError('');
          lastWordRef.current = '';
        })
        .catch(err => {
          if (cancelled) return;
          console.error(err);
          setLoadError(err.message || 'Could not load this file.');
          setContent('');
        })
        .finally(() => {
          if (!cancelled) setIsLoading(false);
        });
      return () => { cancelled = true; };
    }
  }, [aiPreview, filepath]);

  // Freeze whatever cursor a deep link asked for at the moment this file was
  // opened — captured once per file, ignoring the prop's later changes (which
  // just track the user's own subsequent cursor movement and must not hijack
  // their scroll position). Applied from handleEditorMount below, since that
  // fires once Monaco itself is ready — not tied to the [content] effect,
  // which can (and does) run before Monaco's own async mount completes.
  const pendingCursorRef = useRef(null);
  useEffect(() => {
    pendingCursorRef.current = initialCursor;
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [filepath]);

  const handleEditorMount = (editor, monaco) => {
    editorRef.current = editor;

    const cursor = pendingCursorRef.current;
    if (cursor && (cursor.line || cursor.column)) {
      const line = cursor.line || 1;
      const column = cursor.column || 1;
      editor.setPosition({ lineNumber: line, column });
      editor.revealLineInCenter(line);
    }

    // Coding fonts load asynchronously as web fonts (see index.html). Monaco
    // measures glyph widths once at mount, so a font arriving after that would
    // render mismeasured (and ligatures wouldn't kick in) until the next
    // relayout — remeasure as soon as the browser reports fonts are ready.
    if (document.fonts?.ready) {
      document.fonts.ready.then(() => monaco.editor.remeasureFonts());
    }

    // Wire up global helpers for ChatPanel Diff logic
    // eslint-disable-next-line react-hooks/immutability
    window.editor = editor;
    // eslint-disable-next-line react-hooks/immutability
    window.getCurrentContent = () => editor.getValue();
    // eslint-disable-next-line react-hooks/immutability
    window.getSelectedText = () => editor.getModel()?.getValueInRange(editor.getSelection()) ?? '';

    // Ctrl+S / Cmd+S save
    editor.addCommand(monaco.KeyMod.CtrlCmd | monaco.KeyCode.KeyS, () => {
      handleSave(editor.getValue());
    });

    // ── Cursor position changes ────────────────────────────────────────
    editor.onDidChangeCursorPosition((e) => {
      const { lineNumber, column } = e.position;

      if (onCursorChange) {
        onCursorChange(lineNumber, column);
      }

      // Detect word-at-cursor for symbol tracking
      if (onSymbolChange) {
        const model = editor.getModel();
        if (model) {
          const wordInfo = model.getWordAtPosition(e.position);
          const word = wordInfo ? wordInfo.word : '';
          if (word !== lastWordRef.current) {
            lastWordRef.current = word;
            if (word && /^[A-Z]/.test(word)) {
              // PascalCase: treat as a symbol
              onSymbolChange({
                name: word,
                kind: 'unknown',
                parent: '',
                file_path: filepath,
                start_line: lineNumber,
                end_line: lineNumber,
              });
            } else {
              onSymbolChange(null);
            }
          }
        }
      }
    });

    // ── Selection changes ──────────────────────────────────────────────
    editor.onDidChangeCursorSelection((e) => {
      if (onSelectionChange) {
        const model = editor.getModel();
        if (model) {
          const sel = e.selection;
          const text = model.getValueInRange(sel);
          onSelectionChange(
            sel.startLineNumber,
            sel.endLineNumber,
            text
          );
        }
      }
    });

    // ── Content changes ────────────────────────────────────────────────
    editor.onDidChangeModelContent((e) => {
      if (onEdit && filepath && e.changes.length > 0) {
        const firstChange = e.changes[0];
        const lastChange = e.changes[e.changes.length - 1];
        onEdit(
          filepath,
          firstChange.range.startLineNumber,
          lastChange.range.endLineNumber
        );
      }
    });
  };

  const handleSave = async (val) => {
    try {
      const res = await fetch('/api/file', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ path: filepath, content: val })
      });
      if (res.ok) {
        setUnsaved(false);
      }
    } catch (e) {
      console.error(e);
    }
  };

  const onChange = (val) => {
    setContent(val);
    setUnsaved(true);
  };

  const editorOptions = useMemo(() => ({
    minimap: { enabled: editorSettings.minimap ?? false },
    stickyScroll: { enabled: true },
    wordWrap: editorSettings.wordWrap ?? 'on',
    bracketPairColorization: { enabled: editorSettings.bracketPairColorization ?? true },
    formatOnPaste: true,
    formatOnType: editorSettings.formatOnType ?? true,
    smoothScrolling: editorSettings.smoothScrolling ?? true,
    cursorBlinking: editorSettings.cursorBlinking ?? 'smooth',
    renderLineHighlight: editorSettings.renderLineHighlight ?? 'all',
    renderWhitespace: editorSettings.renderWhitespace ?? 'none',
    autoClosingBrackets: editorSettings.autoClosingBrackets ?? 'always',
    codeLens: editorSettings.codeLens ?? true,
    linkedEditing: editorSettings.linkedEditing ?? true,
    matchBrackets: editorSettings.matchBrackets ?? 'always',
    fontFamily: fontFamilyValue(fontSettings.fontFamilyId),
    fontSize: fontSettings.fontSize,
    lineHeight: Math.round(fontSettings.fontSize * fontSettings.lineHeight),
    letterSpacing: fontSettings.letterSpacing,
    fontLigatures: fontSettings.ligatures,
  }), [editorSettings, fontSettings]);

  return (
    <div style={{ width: '100%', height: '100%', display: 'flex', flexDirection: 'column' }}>
      {unsaved && (
        <Text style={{ padding: '0 16px', color: token.colorWarning, fontWeight: 'bold', fontSize: '12px' }}>
          Unsaved changes (Ctrl+S to save)
        </Text>
      )}
      {loadError && <Text style={{ padding: '6px 16px', color: token.colorError, fontSize: 12 }}>{loadError}</Text>}
      <div style={{ flex: 1, minHeight: 0, height: '100%' }}>
        {isLoading ? (
          <div style={{ padding: '24px' }}>
            <Skeleton active paragraph={{ rows: 12 }} />
          </div>
        ) : (
          <Editor
            key={filepath}
            height="100%"
            path={filepath}
            theme={darkMode ? 'vs-dark' : 'vs'}
            value={content}
            onChange={onChange}
            onMount={handleEditorMount}
            options={editorOptions}
          />
        )}
      </div>
    </div>
  );
}
