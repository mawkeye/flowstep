-- Parallel states: add active_in_parallel and parallel_clock to instances
-- Apply with: psql -f 003_parallel_states.sql

ALTER TABLE flowstep_instances ADD COLUMN IF NOT EXISTS active_in_parallel JSONB DEFAULT '{}';
ALTER TABLE flowstep_instances ADD COLUMN IF NOT EXISTS parallel_clock BIGINT DEFAULT 0;
