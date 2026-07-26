import React, { useEffect, useRef, useState } from 'react';
import { Button } from 'antd';
import { CheckOutlined, CloseOutlined, LoadingOutlined, PauseOutlined, PlayCircleOutlined, StopOutlined } from '@ant-design/icons';

/**
 * IndexActivityWidget
 * --------------------
 * The notification panel's contents — one compact card per background
 * process (code scan, AI enrichment). A card in the 'pending_confirmation'
 * state asks the user to approve starting/continuing enrichment instead of
 * just showing progress, since that step calls the AI model on every file.
 *
 * Props
 *   processes       Array<ProcessCard>
 *   onStop          () => void        stop the active scan/enrichment
 *   onApprove       () => void        approve a pending enrichment run
 *   onDecline       () => void        dismiss a pending enrichment run ("Not now")
 *   onPause         () => void        suspend a running enrichment (queue kept)
 *   onResume        () => void        continue a paused enrichment
 *
 * ProcessCard shape:
 *   id        string
 *   phase     string           'scan' | 'enrichment' | 'aggregation'
 *   status    string           'running' | 'done' | 'error' | 'pending_confirmation'
 *   label     string
 *   detail    string
 *   stats     Object<string,string>
 *   progress  number | null    0-100 or null = indeterminate
 */
export default function IndexActivityWidget({ processes = [], onStop, onApprove, onDecline, onPause, onResume }) {
  const [dismissed, setDismissed] = useState({});
  const dismissTimers = useRef({});

  // Auto-dismiss completed cards after 4s — never auto-dismiss a card
  // waiting on the user's decision.
  useEffect(() => {
    processes.forEach(p => {
      if (p.status === 'done' && !dismissTimers.current[p.id]) {
        dismissTimers.current[p.id] = setTimeout(() => {
          setDismissed(prev => ({ ...prev, [p.id]: true }));
          delete dismissTimers.current[p.id];
        }, 4000);
      }
      if (p.status !== 'done' && dismissTimers.current[p.id]) {
        clearTimeout(dismissTimers.current[p.id]);
        delete dismissTimers.current[p.id];
        setDismissed(prev => { const n = { ...prev }; delete n[p.id]; return n; });
      }
    });
  }, [processes]);

  useEffect(() => () => {
    Object.values(dismissTimers.current).forEach(clearTimeout);
  }, []);

  const visible = processes.filter(p => !dismissed[p.id]);
  if (visible.length === 0) return null;

  return (
    <div className="idx-list" role="status" aria-label="Background processes">
      {visible.map(proc => {
        const pending = proc.status === 'pending_confirmation';
        return (
          <div key={proc.id} className={`idx-card idx-status-${proc.status}`}>
            <div className="idx-card-header">
              {proc.status === 'running' ? <LoadingOutlined className="idx-status-icon idx-status-icon-running" /> : (
                <span className={`idx-status-dot idx-status-dot-${proc.status}`} />
              )}
              <span className="idx-card-title">{proc.label}</span>
              {onPause && proc.pausable && proc.status === 'running' && (
                <button className="idx-icon-btn" onClick={onPause} title="Pause" aria-label="Pause">
                  <PauseOutlined />
                </button>
              )}
              {onResume && proc.status === 'paused' && (
                <button className="idx-icon-btn" onClick={onResume} title="Resume" aria-label="Resume">
                  <PlayCircleOutlined />
                </button>
              )}
              {onStop && (proc.status === 'running' || proc.status === 'paused') && (
                <button className="idx-icon-btn" onClick={onStop} title="Stop" aria-label="Stop">
                  <StopOutlined />
                </button>
              )}
            </div>

            {proc.progress != null && (
              <div className="idx-progress-track">
                <div className="idx-progress-fill" style={{ width: `${Math.min(100, proc.progress)}%` }} />
              </div>
            )}

            {proc.detail && (
              <div className="idx-card-detail" title={proc.detail}>{basename(proc.detail)}</div>
            )}

            {pending ? (
              <div className="idx-confirm-row">
                <span className="idx-confirm-hint">This calls the AI model for each file — nothing runs until you approve it.</span>
                <div className="idx-confirm-actions">
                  <Button size="small" type="text" onClick={onDecline} icon={<CloseOutlined />}>Not now</Button>
                  <Button size="small" type="primary" onClick={onApprove} icon={<CheckOutlined />}>Start enriching</Button>
                </div>
              </div>
            ) : proc.stats && Object.keys(proc.stats).length > 0 && (
              <div className="idx-stats-row">
                {Object.entries(proc.stats).map(([k, v]) => (
                  <span key={k} className="idx-stat"><span className="idx-stat-label">{k}</span><span className="idx-stat-value">{v}</span></span>
                ))}
              </div>
            )}
          </div>
        );
      })}
    </div>
  );
}

function basename(path) {
  if (!path) return '';
  const parts = path.replace(/\\/g, '/').split('/');
  return parts[parts.length - 1] || path;
}
