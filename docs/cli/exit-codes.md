# Exit codes

| Code | Meaning |
|---:|---|
| 0 | Success |
| 2 | Parse or input failure |
| 3 | Policy or approval denial |
| 4 | Discord failure |
| 5 | Reconciliation or conflict |
| 6 | Bounded timeout |
| 7 | Internal failure |
| 130 | Operator interruption |

Classification uses typed errors (`errors.Is`/`errors.As`), never error text.
