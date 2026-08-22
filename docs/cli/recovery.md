# Recovery

`job list` and `job get` expose bounded durable metadata derived from saved plans and hash-validated journals. `job cancel` persists cooperative cancellation intent. `job resume` delegates to reconciliation recovery and requires an exact full plan hash; partial or uncertain operations remain conflict-gated.
