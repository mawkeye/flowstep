# Order Workflow

```mermaid
stateDiagram-v2
    [*] --> CREATED
    CREATED --> CANCELLED : cancel
    PROCESSING --> CANCELLED : cancel
    PROCESSING --> DONE : complete
    CREATED --> PROCESSING : start_processing
    CANCELLED --> [*]
    DONE --> [*]
```
