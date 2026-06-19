-- Second schema file, referencing the first file's table by FK — proves the
-- multi-file schema list is loaded as one combined catalog (a table in file 2
-- can reference a table defined in file 1).
CREATE TABLE IF NOT EXISTS customer_order
(
    order_id    integer NOT NULL,
    customer_id integer NOT NULL REFERENCES customer (customer_id),
    total       integer NOT NULL
);
