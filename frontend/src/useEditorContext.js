import { useState, useCallback } from 'react';

/**
 * useEditorContext tracks all editor state client-side and provides
 * a stable context object to be sent with each chat request.
 *
 * Tracks:
 *  - active_file: the currently focused file path
 *  - open_files: all currently open tabs
 *  - cursor: line and column
 *  - selection: highlighted range + text
 *  - selected_symbol: word/symbol at cursor
 *  - recent_files: last 10 distinct files visited
 *  - recent_edits: last 20 edits, one latest-edit per file
 */
export function useEditorContext() {
  const [context, setContext] = useState({
    active_file: '',
    open_files: [],
    recent_files: [],
    cursor: { line: 1, column: 1 },
    selection: null,
    selected_symbol: null,
    recent_edits: [],
  });

  /** Update the active (focused) file. Also adds to open_files and recent_files. */
  const setActiveFile = useCallback((path) => {
    setContext(prev => {
      const recentFiles = [path, ...prev.recent_files.filter(f => f !== path)].slice(0, 10);
      return {
        ...prev,
        active_file: path,
        recent_files: recentFiles,
      };
    });
  }, []);

  /** Update cursor position (1-indexed line and column). */
  const updateCursor = useCallback((line, column) => {
    setContext(prev => ({
      ...prev,
      cursor: { line, column },
    }));
  }, []);

  /** Update selection range and text. Pass null to clear. */
  const updateSelection = useCallback((startLine, endLine, text) => {
    if (!text || text.trim() === '') {
      setContext(prev => ({ ...prev, selection: null }));
      return;
    }
    setContext(prev => ({
      ...prev,
      selection: { start_line: startLine, end_line: endLine, text },
    }));
  }, []);

  /** Update the symbol at the cursor. Pass null to clear. */
  const updateSelectedSymbol = useCallback((symbol) => {
    setContext(prev => ({ ...prev, selected_symbol: symbol }));
  }, []);

  /** Record an edit event for a file (deduplicates per file, keeps latest). */
  const recordEdit = useCallback((file, startLine, endLine) => {
    if (!file) return;
    const lines = startLine === endLine ? `${startLine}` : `${startLine}-${endLine}`;
    setContext(prev => {
      const withoutFile = prev.recent_edits.filter(e => e.file !== file);
      const updated = [{ file, lines }, ...withoutFile].slice(0, 20);
      return { ...prev, recent_edits: updated };
    });
  }, []);

  /** Add a file to open_files (deduplicated). */
  const addOpenFile = useCallback((path) => {
    setContext(prev => {
      if (prev.open_files.includes(path)) return prev;
      return { ...prev, open_files: [...prev.open_files, path] };
    });
  }, []);

  /** Remove a file from open_files. */
  const removeOpenFile = useCallback((path) => {
    setContext(prev => ({
      ...prev,
      open_files: prev.open_files.filter(f => f !== path),
    }));
  }, []);

  return {
    context,
    setActiveFile,
    updateCursor,
    updateSelection,
    updateSelectedSymbol,
    recordEdit,
    addOpenFile,
    removeOpenFile,
  };
}
