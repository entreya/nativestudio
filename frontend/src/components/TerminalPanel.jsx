import React, { useEffect, useRef } from 'react';
import { Terminal } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import '@xterm/xterm/css/xterm.css';
import { useAppState } from '../state/useAppState';

// A real, persistent terminal for the active project: a websocket bridges
// straight to a PTY-backed shell on the server (terminal/session.go), so cd
// persists, long-running processes (npm run dev) survive this panel
// unmounting, and Ctrl+C/colors/interactive prompts all work like a real
// terminal — unlike the AI agent's own run_terminal tool, which is a
// separate, isolated, one-shot mechanism that intentionally does not share
// this shell. The agent's terminal activity is still shown here (printed in,
// not typed into, this shell) via terminalActivity, so the user can watch
// what the AI ran without the two ever fighting over the same input.
export default function TerminalPanel({ projectId, darkMode }) {
  const { terminalActivity } = useAppState();
  const containerRef = useRef(null);
  const termRef = useRef(null);
  const fitRef = useRef(null);
  const wsRef = useRef(null);
  const printedActivityIds = useRef(new Set());

  useEffect(() => {
    if (!containerRef.current || !projectId) return undefined;

    const term = new Terminal({
      convertEol: true,
      fontSize: 13,
      fontFamily: 'SF Mono, Menlo, Consolas, monospace',
      theme: darkMode
        ? { background: '#1e1c1a', foreground: '#e8e2d8' }
        : { background: '#fffdf8', foreground: '#2f2a26' },
      cursorBlink: true,
    });
    const fitAddon = new FitAddon();
    term.loadAddon(fitAddon);
    term.open(containerRef.current);
    fitAddon.fit();
    termRef.current = term;
    fitRef.current = fitAddon;

    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const ws = new WebSocket(`${protocol}//${window.location.host}/api/projects/${projectId}/terminal/ws`);
    wsRef.current = ws;

    // The socket takes a moment to finish its handshake — keystrokes typed
    // in that window (very common right after toggling the panel open)
    // must not be silently dropped, so queue them and flush in order once
    // it's actually open rather than gating each send on readyState alone.
    let queued = [];
    const sendOrQueue = message => {
      if (ws.readyState === WebSocket.OPEN) {
        ws.send(JSON.stringify(message));
      } else {
        queued.push(message);
      }
    };

    ws.onmessage = event => term.write(event.data);
    ws.onopen = () => {
      fitAddon.fit();
      queued.push({ type: 'resize', cols: term.cols, rows: term.rows });
      for (const message of queued) ws.send(JSON.stringify(message));
      queued = [];
    };

    const dataDisposable = term.onData(data => sendOrQueue({ type: 'input', data }));

    const resizeObserver = new ResizeObserver(() => {
      fitAddon.fit();
      sendOrQueue({ type: 'resize', cols: term.cols, rows: term.rows });
    });
    resizeObserver.observe(containerRef.current);

    return () => {
      dataDisposable.dispose();
      resizeObserver.disconnect();
      ws.close();
      term.dispose();
      termRef.current = null;
      wsRef.current = null;
    };
  }, [projectId, darkMode]);

  // Print the AI agent's own run_terminal activity into the same visible
  // terminal — dimmed and prefixed so it reads distinctly from the user's
  // own input/output, without ever being written to the pty itself.
  useEffect(() => {
    const term = termRef.current;
    if (!term) return;
    for (const entry of terminalActivity) {
      if (printedActivityIds.current.has(entry.id)) continue;
      printedActivityIds.current.add(entry.id);
      term.write(`\r\n\x1b[2m[agent] $ ${entry.command}\x1b[0m\r\n`);
      if (entry.output) {
        term.write(entry.output.replace(/\n/g, '\r\n'));
      }
      term.write(`\r\n`);
    }
  }, [terminalActivity]);

  return <div ref={containerRef} style={{ width: '100%', height: '100%', padding: '4px 8px' }} />;
}
