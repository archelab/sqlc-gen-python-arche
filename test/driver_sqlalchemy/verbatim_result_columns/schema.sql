-- Proves singularize_result_columns:false emits RESULT-column field names
-- VERBATIM (the drop-in-from-upstream behaviour). With the option omitted/true
-- these plural aliases would singularize (total_creditos->total_credito,
-- valor_servicos->valor_servico, codigo_barras->codigo_barra, dados->dado) —
-- every other golden exercises that default. A base-TABLE model field
-- (report.dados) is always verbatim regardless of the knob (buildTable path).
CREATE TABLE IF NOT EXISTS report
(
    report_id bigint NOT NULL,
    dados     jsonb  NOT NULL
);
