-- Parallel states: add active_in_parallel and parallel_clock to instances
--
-- Note: apply each migration exactly once. SQLite does not support IF NOT EXISTS
-- for ADD COLUMN in all driver versions; this migration is additive and safe on
-- any schema that has not yet been migrated.
ALTER TABLE flowstep_instances ADD COLUMN active_in_parallel TEXT DEFAULT '{}';
ALTER TABLE flowstep_instances ADD COLUMN parallel_clock INTEGER DEFAULT 0;
