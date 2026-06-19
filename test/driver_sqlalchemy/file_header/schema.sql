-- Proves the F5 file_header primitive (C-#11): with file_header set, the
-- literal is absolute line 1 of EVERY emitted file (models.py, the query file,
-- AND __init__.py) before the sqlc header. Mirrors analytics's `# pyright:
-- basic` header + emit_init_file: true.
CREATE TABLE IF NOT EXISTS gadget
(
    gadget_id bigint NOT NULL,
    label     text   NOT NULL
);
