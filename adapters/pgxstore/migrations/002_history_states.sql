-- History states: add shallow and deep history maps to instances
-- Apply with: psql -f 002_history_states.sql

ALTER TABLE flowstep_instances ADD COLUMN IF NOT EXISTS shallow_history JSONB DEFAULT '{}';
ALTER TABLE flowstep_instances ADD COLUMN IF NOT EXISTS deep_history JSONB DEFAULT '{}';
