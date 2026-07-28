-- Key usage aggregation queries.

-- name: RecordKeyUsage :exec
INSERT INTO key_usage (key_id, date, tokens_in, tokens_out, cost_usd, request_count, model, provider)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(key_id, date, model, provider) DO UPDATE SET
    tokens_in = tokens_in + excluded.tokens_in,
    tokens_out = tokens_out + excluded.tokens_out,
    cost_usd = cost_usd + excluded.cost_usd,
    request_count = request_count + excluded.request_count;

-- name: GetKeyUsage :many
SELECT key_id, date, tokens_in, tokens_out, cost_usd, request_count, model, provider
FROM key_usage
WHERE key_id = ? AND date >= ? AND date <= ?
ORDER BY date DESC, model ASC;

-- name: GetCurrentMonthUsage :one
SELECT COALESCE(SUM(cost_usd), 0) FROM key_usage
WHERE key_id = ? AND date >= ? AND date <= ?;

-- name: GetUsageSummary :one
SELECT COALESCE(SUM(request_count),0), COALESCE(SUM(cost_usd),0), COALESCE(SUM(tokens_in),0), COALESCE(SUM(tokens_out),0) FROM key_usage;
