-- Stores the agent's reasoning trace (thinking, context gathering, tool calls)
-- as the raw ordered event log emitted during the run, serialized as JSON.
-- Without this the timeline lived only in the browser's memory and vanished on
-- refresh, leaving restored conversations as bare answers with no trace of how
-- the agent got there. Replaying the events client-side (rather than storing
-- rendered entries) keeps the restored view identical to the live one.
ALTER TABLE messages ADD COLUMN timeline TEXT NOT NULL DEFAULT '';
