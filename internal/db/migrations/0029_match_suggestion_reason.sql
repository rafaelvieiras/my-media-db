-- +goose Up
-- Migration 0029: the match alert gains a REASON and the counts behind it.
--
-- Migration 0028 modelled ONE reason to warn the owner: a cheaper variant of the name on
-- disk found a candidate ("did you mean this?"). Two more situations deserve the same
-- warning, and both are discovered from data the product already has -- measured in
-- production on 19/nov/2026:
--
--   'empty_tree'    the source answered a series with ZERO episodes. The match is useless
--                   by construction: no file can ever be placed in a tree that has no
--                   episodes. Measured: a phantom TMDb entry (0 seasons, 0 episodes, empty
--                   overview, origin "IO") won the exact-name comparison against the real
--                   series, and 86 files with a clean S01E01 became provisional drafts.
--   'tree_mismatch' the tree EXISTS but does not fit the files: the series accumulated at
--                   least as many provisional episodes as cataloged ones. Measured: a
--                   folder holding another show of the same name (files S01E14+ against a
--                   tree of 13 episodes per season), or a source that numbers the seasons
--                   differently (a season numbered 1940 against a folder "Temporada 01").
--
-- The candidate columns keep their meaning, with one nuance the reader needs:
--   'name' and 'empty_tree' carry a REAL candidate (another item of the same name);
--   'tree_mismatch' carries the CURRENT identity, which is the thing under suspicion.
-- The API resolves that into "no apply button" instead of offering the owner a button that
-- reapplies what is already there.
--
-- files_unmatched/files_total are the evidence that justifies the warning and the numbers
-- the screen shows. They stay 0 for 'name' (a name hint has no file census).
ALTER TABLE match_suggestion ADD COLUMN reason TEXT NOT NULL DEFAULT 'name';
ALTER TABLE match_suggestion ADD COLUMN files_unmatched INTEGER NOT NULL DEFAULT 0;
ALTER TABLE match_suggestion ADD COLUMN files_total INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE match_suggestion DROP COLUMN files_total;
ALTER TABLE match_suggestion DROP COLUMN files_unmatched;
ALTER TABLE match_suggestion DROP COLUMN reason;
