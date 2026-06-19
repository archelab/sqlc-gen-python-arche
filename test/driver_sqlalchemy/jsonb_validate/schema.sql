-- jsonb_validate case: jsonb columns overridden to typed pydantic shapes with
-- `validate: true`. The generated read path validates the stored JSON against the
-- shape (fail-loud) instead of returning it unwrapped.
--   * config  NOT NULL -> WidgetConfig (nested model)        [scalar :one, struct, :many]
--   * payload NOT NULL -> WidgetPayload (discriminated union A|B|C[D])
--   * extra   NULLABLE -> WidgetConfig                       [the NULL-guard path]
CREATE TABLE IF NOT EXISTS widget
(
    widget_id integer NOT NULL,
    config    jsonb   NOT NULL,
    payload   jsonb   NOT NULL,
    extra     jsonb
);
