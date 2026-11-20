-- +goose Up
-- Migration 0030 (issue #62, ADR-0040): the queue becomes OBSERVABLE.
--
-- Until now `queue_item` could say "running, 40 of 120, <last message>" and nothing else. That
-- was not enough to answer the two questions the owner actually asks while a job runs: "what is it
-- doing right now" and "which items already failed". A manual health job once stayed `running` for
-- over an hour with a single `message` column as the only evidence.
--
-- Two additions, both in `queue_item`'s own vocabulary (no per-job tables, AGENTS.md item 3):
--
--   step_index/step_total/step_label  where in the job's pipeline the run is. A scan has one step;
--                                     a job may have several (re-scan, auto-match, retroactive
--                                     enrichment, ...). 0/0 means "no steps reported".
--   current_item                      the item being processed RIGHT NOW (a file path relative to
--                                     the source, an entity title, a source path). NULL when the
--                                     runner does not report item granularity.
--   progress_ok/progress_failed       running counters of items that succeeded/failed in this run.
--                                     progress_current keeps its meaning (items seen, success or
--                                     not) so old readers do not change meaning.
--
-- `queue_item_event` is the append-only trail behind "which ones failed": one row per item the
-- runner finishes, with its outcome. It is bounded: the finish step prunes the oldest rows beyond
-- a per-item cap, so a 5000-file scan cannot grow the database without limit. The table keeps the
-- same lifecycle as its parent (ON DELETE CASCADE) and never outlives the retained history.
ALTER TABLE queue_item ADD COLUMN step_index INTEGER NOT NULL DEFAULT 0;
ALTER TABLE queue_item ADD COLUMN step_total INTEGER NOT NULL DEFAULT 0;
ALTER TABLE queue_item ADD COLUMN step_label TEXT;
ALTER TABLE queue_item ADD COLUMN current_item TEXT;
ALTER TABLE queue_item ADD COLUMN progress_ok INTEGER NOT NULL DEFAULT 0;
ALTER TABLE queue_item ADD COLUMN progress_failed INTEGER NOT NULL DEFAULT 0;

CREATE TABLE queue_item_event (
    id            TEXT PRIMARY KEY,
    queue_item_id TEXT NOT NULL REFERENCES queue_item(id) ON DELETE CASCADE,
    -- seq: monotonic per item, so the trail can be read in order without relying on `at`
    -- (two events in the same millisecond are possible).
    seq           INTEGER NOT NULL,
    at            TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    -- status: 'ok' (done), 'error' (tried, failed), 'skip' (seen, intentionally not processed).
    status        TEXT NOT NULL CHECK (status IN ('ok', 'error', 'skip')),
    -- label: what the row is about -- a file path relative to the source, an entity title, a
    -- source path. Free text, chosen by the runner.
    label         TEXT NOT NULL,
    -- detail: short reason/extra context (the error text, the chosen provider, the score).
    detail        TEXT
);

-- Reading a trail is always "this item, in order"; pruning is "this item, oldest first". One index
-- serves both.
CREATE INDEX idx_queue_item_event_por_item ON queue_item_event(queue_item_id, seq);

-- +goose Down
DROP INDEX idx_queue_item_event_por_item;
DROP TABLE queue_item_event;
ALTER TABLE queue_item DROP COLUMN progress_failed;
ALTER TABLE queue_item DROP COLUMN progress_ok;
ALTER TABLE queue_item DROP COLUMN current_item;
ALTER TABLE queue_item DROP COLUMN step_label;
ALTER TABLE queue_item DROP COLUMN step_total;
ALTER TABLE queue_item DROP COLUMN step_index;
