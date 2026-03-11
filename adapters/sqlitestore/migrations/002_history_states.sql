-- History states: add shallow and deep history maps to instances

-- Note: apply each migration exactly once. SQLite does not support IF NOT EXISTS
-- for ADD COLUMN in all driver versions; this migration is additive and safe on
-- any schema that has not yet been migrated.
ALTER TABLE flowstep_instances ADD COLUMN shallow_history TEXT DEFAULT '{}';
ALTER TABLE flowstep_instances ADD COLUMN deep_history TEXT DEFAULT '{}';
