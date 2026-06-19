-- Pins the keyword-only-vs-bundle boundary (C-#15) at query_parameter_limit: 2
-- and the P-16 flat-arg name de-collision. The case uses qpl=2 (NOT the reference's
-- default 1) precisely to exercise the AT-LIMIT case (params == qpl ->
-- keyword-only) distinctly from the ABOVE-LIMIT case (params > qpl -> bundle),
-- which a qpl=1 config cannot show.
CREATE TABLE IF NOT EXISTS thing
(
    thing_id bigint NOT NULL,
    a        text   NOT NULL,
    b        text   NOT NULL,
    c        text   NOT NULL,
    status   text   NOT NULL
);
