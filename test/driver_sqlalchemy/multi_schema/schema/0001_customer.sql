-- First schema file in a multi-file schema list (mirrors app_billing listing
-- app_db migrations by path).
CREATE TABLE IF NOT EXISTS customer
(
    customer_id integer NOT NULL,
    name        text    NOT NULL
);
