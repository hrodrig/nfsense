-- +goose Up
-- +goose StatementBegin
CREATE TABLE schema_meta (
    key   TEXT PRIMARY KEY CHECK (length(key) BETWEEN 1 AND 64),
    value TEXT NOT NULL
) STRICT;

CREATE TABLE working_draft (
    id             INTEGER PRIMARY KEY CHECK (id = 1),
    revision       INTEGER NOT NULL DEFAULT 0 CHECK (revision >= 0),
    backend        TEXT NOT NULL CHECK (backend IN ('nft')),
    format_version INTEGER NOT NULL CHECK (format_version >= 1),
    document       TEXT NOT NULL
                   CHECK (json_valid(document))
                   CHECK (length(CAST(document AS BLOB)) <= 1048576),
    created_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_by     TEXT NOT NULL DEFAULT ''
) STRICT;

CREATE TABLE applied_revisions (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    draft_revision  INTEGER NOT NULL CHECK (draft_revision >= 0),
    backend         TEXT NOT NULL CHECK (backend IN ('nft')),
    format_version  INTEGER NOT NULL CHECK (format_version >= 1),
    document        TEXT NOT NULL CHECK (json_valid(document)),
    artifact        TEXT NOT NULL,
    artifact_sha256 TEXT NOT NULL,
    status          TEXT NOT NULL
                    CHECK (status IN ('applied', 'confirmed', 'rolled_back', 'failed')),
    actor           TEXT NOT NULL,
    applied_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
) STRICT;

CREATE INDEX applied_revisions_applied_at_idx ON applied_revisions (applied_at DESC);

INSERT INTO schema_meta (key, value) VALUES
    ('store_format_version', '1'),
    ('store_created_at', strftime('%Y-%m-%dT%H:%M:%fZ', 'now'));

-- D-17: the singleton draft row is created by the schema, never synthesized by
-- Go, so a missing row is a fault rather than a first-run condition.
INSERT INTO working_draft (id, revision, backend, format_version, document)
VALUES (1, 0, 'nft', 1, '{"format":"nfsense.draft","format_version":1,"body":{}}');
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX applied_revisions_applied_at_idx;
DROP TABLE applied_revisions;
DROP TABLE working_draft;
DROP TABLE schema_meta;
-- +goose StatementEnd
