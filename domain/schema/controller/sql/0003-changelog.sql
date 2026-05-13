CREATE TABLE change_log_edit_type (
    id INT PRIMARY KEY,
    edit_type TEXT
);

CREATE UNIQUE INDEX idx_change_log_edit_type_edit_type
ON change_log_edit_type (edit_type);

-- The change log type values are bitmasks, so that multiple types can be
-- expressed when looking for changes.
INSERT INTO change_log_edit_type VALUES
(1, 'create'),
(2, 'update'),
(4, 'delete');

CREATE TABLE change_log_namespace (
    id INT PRIMARY KEY,
    namespace TEXT,
    description TEXT
);

CREATE UNIQUE INDEX idx_change_log_namespace_namespace
ON change_log_namespace (namespace);

CREATE TABLE change_log (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    edit_type_id INT NOT NULL,
    namespace_id INT NOT NULL,
    changed TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT (STRFTIME('%Y-%m-%d %H:%M:%f', 'NOW', 'utc')),
    txn_id   INTEGER NOT NULL DEFAULT 0,
    trace_id TEXT    NOT NULL DEFAULT '',
    span_id  TEXT    NOT NULL DEFAULT '',
    CONSTRAINT fk_change_log_edit_type
    FOREIGN KEY (edit_type_id)
    REFERENCES change_log_edit_type (id),
    CONSTRAINT fk_change_log_namespace
    FOREIGN KEY (namespace_id)
    REFERENCES change_log_namespace (id)
);

-- The change log witness table is used to track which nodes have seen
-- which change log entries. This is used to determine when a change log entry
-- can be deleted.
-- We'll delete all change log entries that are older than the lower_bound
-- change log entry that has been seen by all controllers.
CREATE TABLE change_log_witness (
    controller_id TEXT NOT NULL PRIMARY KEY,
    lower_bound INT NOT NULL DEFAULT (-1),
    upper_bound INT NOT NULL DEFAULT (-1),
    updated_at DATETIME NOT NULL DEFAULT (STRFTIME('%Y-%m-%d %H:%M:%f', 'NOW', 'utc'))
);

-- Monotonically increasing transaction sequence. Incremented once per
-- write transaction as the last operation before COMMIT, then used to
-- back-fill change_log.txn_id for all rows written in that
-- transaction. txn_id = 0 is the "not yet stamped" sentinel; the
-- sequence begins at 1 after the first increment.
CREATE TABLE IF NOT EXISTS change_log_txn_seq (
    id INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (id)
);
INSERT OR IGNORE INTO change_log_txn_seq VALUES (0);

-- Holds the OTel trace context active during the current write
-- transaction. Written by the DB transaction wrapper after BEGIN;
-- reset before COMMIT. The is_in_txn sentinel tells triggers whether
-- this write occurred inside a managed transaction.
CREATE TABLE IF NOT EXISTS change_log_trace_ctx (
    id        INTEGER PRIMARY KEY DEFAULT 1 CHECK(id = 1),
    is_in_txn INTEGER NOT NULL DEFAULT 0,
    trace_id  TEXT    NOT NULL DEFAULT '',
    span_id   TEXT    NOT NULL DEFAULT ''
);
INSERT OR IGNORE INTO change_log_trace_ctx VALUES (1, 0, '', '');

-- Controls the debug state of the changestream for this database.
-- Only superusers may write to this table via the API.
-- The id = 1 constraint enforces the single-row invariant.
CREATE TABLE IF NOT EXISTS debug_change_stream (
    id          INTEGER PRIMARY KEY DEFAULT 1 CHECK(id = 1),
    state       TEXT    NOT NULL DEFAULT 'running'
        CHECK(state IN ('running', 'paused', 'step')),
    step_target INTEGER NOT NULL DEFAULT 0,
    updated_at  DATETIME NOT NULL
        DEFAULT (STRFTIME('%Y-%m-%d %H:%M:%f', 'NOW', 'utc'))
);
INSERT OR IGNORE INTO debug_change_stream
    VALUES (1, 'running', 0,
            STRFTIME('%Y-%m-%d %H:%M:%f', 'NOW', 'utc'));

CREATE INDEX IF NOT EXISTS idx_change_log_unstamped
    ON change_log(txn_id) WHERE txn_id = 0;

-- Stamps trace context onto each new change_log row from
-- change_log_trace_ctx. Only stamps when is_in_txn = 1, so
-- out-of-band writes (migrations, direct SQL) receive empty strings.
CREATE TRIGGER IF NOT EXISTS change_log_set_trace
AFTER INSERT ON change_log
BEGIN
    UPDATE change_log
    SET
        trace_id = CASE
            WHEN (SELECT is_in_txn FROM change_log_trace_ctx) = 1
            THEN (SELECT trace_id FROM change_log_trace_ctx)
            ELSE '' END,
        span_id  = CASE
            WHEN (SELECT is_in_txn FROM change_log_trace_ctx) = 1
            THEN (SELECT span_id FROM change_log_trace_ctx)
            ELSE '' END
    WHERE id = NEW.id;
END;
