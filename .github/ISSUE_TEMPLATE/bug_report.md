---
name: Bug Report
about: Something is broken or behaving unexpectedly
title: '[bug] '
labels: bug
assignees: ''
---

## What happened?

<!-- Clear description of the bug. What did you expect? What did you get? -->

## Steps to Reproduce

```
1.
2.
3.
```

## Environment

| Field | Value |
|-------|-------|
| ILTER version / commit | |
| OS | |
| Go version (if building from source) | |
| Deployment | binary / docker / docker-compose / kubernetes |
| Providers configured | OpenAI / Anthropic / Ollama / ... |
| Redis enabled? | yes / no |

## Relevant Logs

```
# ILTER_LOG_LEVEL=debug ./ilter serve 2>&1
paste relevant log lines here
```

## Additional Context

<!-- Middleware that's active, request payload structure, anything that helps narrow it down -->
