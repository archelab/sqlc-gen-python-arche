-- Proves P-11 (C-#16): a PG ENUM emits `class <Name>(enum.StrEnum):` into
-- models.py with PascalCase class name, member names from the VALUE (uppercased
-- + sanitized), and double-quoted string values. Fixes the four upstream
-- sqlc-gen-python StrEnum defects (#47 class name, #49 member names, #51 Any
-- values, #4 unquoted values). Insurance — a typical consumer has zero PG ENUM columns. The
-- `mixed-case` value with a hyphen exercises the member sanitization.
CREATE TYPE order_status AS ENUM ('pending', 'shipped', 'out-for-delivery', 'delivered');

CREATE TABLE IF NOT EXISTS orders
(
    order_id bigint       NOT NULL,
    status   order_status NOT NULL
);
