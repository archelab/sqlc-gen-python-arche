-- Synthetic case for P-03 (C-#13): a :many whose SQL root is a DML statement
-- (INSERT/UPDATE/DELETE ... RETURNING). a typical consumer has ZERO such queries today, so
-- this golden authors the latent shape: Postgres rejects a server-side cursor
-- (conn.stream) for a DML+RETURNING statement, so the :many must materialize
-- eagerly via conn.execute(...).all() and return list[T], NOT conn.stream.
CREATE TABLE IF NOT EXISTS widget
(
    widget_id bigint      NOT NULL,
    name      text        NOT NULL,
    status    text        NOT NULL
);
