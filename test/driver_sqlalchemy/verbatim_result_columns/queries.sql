-- name: GetReportTotals :one
SELECT
    count(*)::bigint        AS total_creditos,
    count(*)::bigint        AS total_documentos,
    sum(report_id)::bigint  AS valor_servicos,
    max(report_id)::bigint  AS codigo_barras
FROM report;

-- name: GetReportRow :one
SELECT report_id, dados FROM report WHERE report_id = sqlc.arg(report_id)::bigint;
