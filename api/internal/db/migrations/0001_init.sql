-- Times are stored as Unix milliseconds (UTC). Ids are random UUIDv4 strings.

CREATE TABLE users (
    id            TEXT    PRIMARY KEY,
    email         TEXT    NOT NULL UNIQUE COLLATE NOCASE,
    password_hash TEXT    NOT NULL,
    created_at    INTEGER NOT NULL
) STRICT;

CREATE TABLE sessions (
    id           TEXT    PRIMARY KEY,
    user_id      TEXT    NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash   BLOB    NOT NULL UNIQUE,
    device_name  TEXT    NOT NULL,
    created_at   INTEGER NOT NULL,
    last_used_at INTEGER NOT NULL,
    expires_at   INTEGER NOT NULL, -- absolute expiry; sliding expiry is last_used_at + 30 days
    revoked_at   INTEGER
) STRICT;
CREATE INDEX sessions_user ON sessions(user_id);

CREATE TABLE projects (
    id          TEXT    PRIMARY KEY,
    user_id     TEXT    NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name        TEXT    NOT NULL,
    description TEXT    NOT NULL DEFAULT '',
    position    INTEGER NOT NULL,
    archived_at INTEGER,
    created_at  INTEGER NOT NULL,
    updated_at  INTEGER NOT NULL
) STRICT;
CREATE INDEX projects_user ON projects(user_id, position);

CREATE TABLE log_entries (
    id         TEXT    PRIMARY KEY,
    project_id TEXT    NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    body       TEXT    NOT NULL,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
) STRICT;
CREATE INDEX log_entries_project ON log_entries(project_id, created_at);

CREATE TABLE tasks (
    id           TEXT    PRIMARY KEY,
    project_id   TEXT    NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    title        TEXT    NOT NULL,
    done         INTEGER NOT NULL DEFAULT 0 CHECK (done IN (0, 1)),
    position     INTEGER NOT NULL,
    completed_at INTEGER,
    created_at   INTEGER NOT NULL,
    updated_at   INTEGER NOT NULL
) STRICT;
CREATE INDEX tasks_project ON tasks(project_id, position);

CREATE TABLE tags (
    id         TEXT    PRIMARY KEY,
    user_id    TEXT    NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name       TEXT    NOT NULL,
    created_at INTEGER NOT NULL
) STRICT;
-- Full Unicode case-insensitive uniqueness is enforced in the store layer;
-- this index is the ASCII backstop.
CREATE UNIQUE INDEX tags_user_name ON tags(user_id, name COLLATE NOCASE);

CREATE TABLE project_tags (
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    tag_id     TEXT NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
    PRIMARY KEY (project_id, tag_id)
) STRICT;
CREATE INDEX project_tags_tag ON project_tags(tag_id);

-- A tag that is no longer attached to any project is deleted automatically.
-- Foreign-key cascades fire this trigger too, so deleting a project cleans up.
CREATE TRIGGER project_tags_gc AFTER DELETE ON project_tags
BEGIN
    DELETE FROM tags
     WHERE id = OLD.tag_id
       AND NOT EXISTS (SELECT 1 FROM project_tags WHERE tag_id = OLD.tag_id);
END;

CREATE TABLE auth_events (
    id         TEXT    PRIMARY KEY,
    user_id    TEXT    REFERENCES users(id) ON DELETE SET NULL,
    kind       TEXT    NOT NULL,
    ip         TEXT    NOT NULL,
    created_at INTEGER NOT NULL
) STRICT;
CREATE INDEX auth_events_created ON auth_events(created_at);
