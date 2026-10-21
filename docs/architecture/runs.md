# Runs

A run is a durable objective that groups tasks.

Example:

```text
RUN-01K... Implement multiplayer MVP
├── JCF-01K... Protocol
├── JCF-01K... Server
├── JCF-01K... Client
└── JCF-01K... Tests
```

Runs do not own execution. Tasks own work state and sessions own temporary
worker state. A run provides orchestration context, grouping, and eventual
summaries.

Runs use `juicyfruit.run/v1` and are persisted under `.juicyfruit/runs/`.
