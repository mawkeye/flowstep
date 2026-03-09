# Booking Workflow

```mermaid
stateDiagram-v2
    [*] --> CREATED
    CREATED --> AWAITING_PAYMENT : initiate_payment
    AWAITING_PAYMENT --> CANCELLED : payment_fail
    AWAITING_PAYMENT --> CONFIRMED : payment_ok
    CANCELLED --> [*]
    CONFIRMED --> [*]
```
