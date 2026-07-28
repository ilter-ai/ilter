-- Daily usage aggregation queries.

-- name: RecordDailyUsage :exec
INSERT INTO usage_daily (key_id, date, model, provider, tokens, cost, request_count, prompt_tokens, completion_tokens, cache_hits)
VALUES (?, ?, ?, ?, ?, ?, 1, ?, ?, ?)
ON CONFLICT(key_id, date, model, provider) DO UPDATE SET
    tokens = tokens + excluded.tokens,
    cost = cost + excluded.cost,
    request_count = request_count + 1,
    prompt_tokens = prompt_tokens + excluded.prompt_tokens,
    completion_tokens = completion_tokens + excluded.completion_tokens,
    cache_hits = cache_hits + excluded.cache_hits;
