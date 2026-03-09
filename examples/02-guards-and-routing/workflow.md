# Loan Workflow

```mermaid
stateDiagram-v2
    [*] --> SUBMITTED
    SUBMITTED --> MANUAL_REVIEW : evaluate
    SUBMITTED --> AUTO_APPROVED : evaluate
    AUTO_APPROVED --> [*]
    MANUAL_REVIEW --> [*]
```
