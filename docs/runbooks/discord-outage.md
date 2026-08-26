# Discord outage

Owner: Operations
Last reviewed: 2026-08-25

1. Confirm Discord REST error ratio, 429 retries, queue depth, and interaction deadlines.
2. Keep configured bounded retries/backoff; do not increase concurrency during rate limiting.
3. Pause new automation mutations when queues or deadlines exceed budgets.
4. Never replay an uncertain create; reconcile by logical identity and require review on zero/multiple matches.
5. Preserve dead letters and audit correlations, then confirm queue recovery and no duplicate mutations.

Run the fixture drill in [readiness drills](readiness-drills.md); live Discord credentials are not accepted.
