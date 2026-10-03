# Optimistic concurrency on full rewrite
- Instead of: last write wins.
- Why: detects a human edit made mid-session instead of silently losing it.
- Cost: the model re-reads and retries on conflict.
