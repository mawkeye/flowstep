-- flowstep schema for PostgreSQL
-- Apply with: psql -f 001_flowstep.sql

CREATE TABLE IF NOT EXISTS flowstep_events (
    id              TEXT PRIMARY KEY,
    aggregate_type  TEXT NOT NULL,
    aggregate_id    TEXT NOT NULL,
    workflow_type   TEXT NOT NULL,
    workflow_version INT NOT NULL DEFAULT 1,
    event_type      TEXT NOT NULL,
    correlation_id  TEXT NOT NULL,
    causation_id    TEXT,
    actor_id        TEXT,
    transition_name TEXT,
    state_before    JSONB,
    state_after     JSONB,
    payload         JSONB,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_events_aggregate
    ON flowstep_events (aggregate_type, aggregate_id, created_at);
CREATE INDEX IF NOT EXISTS idx_events_correlation
    ON flowstep_events (correlation_id, created_at);

CREATE TABLE IF NOT EXISTS flowstep_instances (
    id              TEXT PRIMARY KEY,
    workflow_type   TEXT NOT NULL,
    workflow_version INT NOT NULL DEFAULT 1,
    aggregate_type  TEXT NOT NULL,
    aggregate_id    TEXT NOT NULL UNIQUE,
    current_state   TEXT NOT NULL,
    state_data      JSONB DEFAULT '{}',
    correlation_id  TEXT NOT NULL,
    is_stuck        BOOLEAN DEFAULT FALSE,
    stuck_reason    TEXT,
    retry_count     INT DEFAULT 0,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_instances_aggregate
    ON flowstep_instances (aggregate_type, aggregate_id);

CREATE TABLE IF NOT EXISTS flowstep_tasks (
    id              TEXT PRIMARY KEY,
    workflow_type   TEXT NOT NULL,
    aggregate_type  TEXT NOT NULL,
    aggregate_id    TEXT NOT NULL,
    correlation_id  TEXT NOT NULL,
    task_type       TEXT NOT NULL,
    description     TEXT,
    options         JSONB,
    status          TEXT NOT NULL DEFAULT 'PENDING',
    choice          TEXT,
    completed_by    TEXT,
    timeout         BIGINT DEFAULT 0,
    expires_at      TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at    TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_tasks_aggregate
    ON flowstep_tasks (aggregate_type, aggregate_id);

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
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at            TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_children_child
    ON flowstep_children (child_aggregate_type, child_aggregate_id);
CREATE INDEX IF NOT EXISTS idx_children_parent
    ON flowstep_children (parent_aggregate_type, parent_aggregate_id);
CREATE INDEX IF NOT EXISTS idx_children_group
    ON flowstep_children (group_id) WHERE group_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS flowstep_activities (
    id              TEXT PRIMARY KEY,
    activity_name   TEXT NOT NULL,
    workflow_type   TEXT NOT NULL,
    aggregate_type  TEXT NOT NULL,
    aggregate_id    TEXT NOT NULL,
    correlation_id  TEXT NOT NULL,
    mode            TEXT NOT NULL,
    input           JSONB,
    output          JSONB,
    error_msg       TEXT,
    retry_policy    JSONB,
    timeout         BIGINT DEFAULT 0,
    status          TEXT NOT NULL DEFAULT 'SCHEDULED',
    max_attempts    INT DEFAULT 1,
    attempt_count   INT DEFAULT 0,
    scheduled_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    started_at      TIMESTAMPTZ,
    completed_at    TIMESTAMPTZ,
    next_retry_at   TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_activities_aggregate
    ON flowstep_activities (aggregate_type, aggregate_id);
