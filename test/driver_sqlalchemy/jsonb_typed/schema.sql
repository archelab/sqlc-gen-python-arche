-- Proves the P-09 jsonb policy: jsonb defaults to typing.Any (matching the reference
-- models.py Optional[Any]), but a per-column override can map a jsonb column to
-- a typed pydantic model where the JSON shape is known. `payload` keeps the
-- default (-> typing.Any); `config` is overridden to a typed model
-- (-> shapes.WidgetConfig).
CREATE TABLE IF NOT EXISTS widget
(
    widget_id integer NOT NULL,
    payload   jsonb,
    config    jsonb   NOT NULL
);
