-- flowstep schema for SQLite
-- Apply with: sqlite3 flowstep.db < 001_flowstep.sql

CREATE TABLE IF NOT EXISTS flowstep_events (
    id              TEXT PRIMARY KEY,
    aggregate_type  TEXT NOT NULL,
    aggregate_id    TEXT NOT NULL,
    workflow_type   TEXT NOT NULL,
    workflow_version INTEGER NOT NULL DEFAULT 1,
    event_type      TEXT NOT NULL,
    correlation_id  TEXT NOT NULL,
    causation_id    TEXT,
    actor_id        TEXT,
    transition_name TEXT,
    state_before    TEXT,
    state_after     TEXT,
    payload         TEXT,
    created_at      TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_events_aggregate
    ON flowstep_events (aggregate_type, aggregate_id, created_at);
CREATE INDEX IF NOT EXISTS idx_events_correlation
    ON flowstep_events (correlation_id, created_at);

CREATE TABLE IF NOT EXISTS flowstep_instances (
    id              TEXT PRIMARY KEY,
    workflow_type   TEXT NOT NULL,
    workflow_version INTEGER NOT NULL DEFAULT 1,
    aggregate_type  TEXT NOT NULL,
    aggregate_id    TEXT NOT NULL UNIQUE,
    current_state   TEXT NOT NULL,
    state_data      TEXT DEFAULT '{}',
    correlation_id  TEXT NOT NULL,
    is_stuck        INTEGER DEFAULT 0,
    stuck_reason    TEXT,
    retry_count     INTEGER DEFAULT 0,
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS flowstep_tasks (
    id              TEXT PRIMARY KEY,
    workflow_type   TEXT NOT NULL,
    aggregate_type  TEXT NOT NULL,
    aggregate_id    TEXT NOT NULL,
    correlation_id  TEXT NOT NULL,
    task_type       TEXT NOT NULL,
    description     TEXT,
    options         TEXT,
    status          TEXT NOT NULL DEFAULT 'PENDING',
    choice          TEXT,
    completed_by    TEXT,
    timeout         INTEGER DEFAULT 0,
    expires_at      TEXT,
    created_at      TEXT NOT NULL,
    completed_at    TEXT
);

CREATE TABLE IF NOT EXISTS flowstep_children (
    id                      TEXT PRIMARY KEY,
    group_id                TEXT,
    parent_workflow_type    TEXT NOT NULL,
    parent_aggregate_type   TEXT NOT NULL,
    parent_aggregate_id     TEXT NOT NULL,
    child_workflow_type     TEXT NOT NULL,
    child_aggregate_type    TEXT NOT NULL,
    child_aggregate_id      TEXT NOT NULL,
    correlation_id          TEXT NOT NULL,
    status                  TEXT NOT NULL DEFAULT 'ACTIVE',
    child_terminal_state    TEXT,
    join_policy             TEXT,
    created_at              TEXT NOT NULL,
    completed_at            TEXT
);

CREATE INDEX IF NOT EXISTS idx_children_child
    ON flowstep_children (child_aggregate_type, child_aggregate_id);
CREATE INDEX IF NOT EXISTS idx_children_parent
    ON flowstep_children (parent_aggregate_type, parent_aggregate_id);

CREATE TABLE IF NOT EXISTS flowstep_activities (
    id              TEXT PRIMARY KEY,
    activity_name   TEXT NOT NULL,
    workflow_type   TEXT NOT NULL,
    aggregate_type  TEXT NOT NULL,
    aggregate_id    TEXT NOT NULL,
    correlation_id  TEXT NOT NULL,
    mode            TEXT NOT NULL,
    input           TEXT,
    output          TEXT,
    error_msg       TEXT,
    retry_policy    TEXT,
    timeout         INTEGER DEFAULT 0,
    status          TEXT NOT NULL DEFAULT 'SCHEDULED',
    max_attempts    INTEGER DEFAULT 1,
    attempt_count   INTEGER DEFAULT 0,
    scheduled_at    TEXT NOT NULL,
    started_at      TEXT,
    completed_at    TEXT
);
