# Juicyfruit Project Bootstrap

This project uses Juicyfruit for durable AI-assisted engineering.

- Canonical work state lives in `.juicyfruit/`.
- Discover or receive a task before working.
- Claim a task before changing implementation state.
- Load only context relevant to the task.
- Persist compact checkpoints as work progresses.
- Verify before marking a task done.
- Treat provider folders such as `.claude/`, `.cursor/`, `.agents/`, and
  `.gemini/` as adapter outputs, not canonical state.

Useful commands:

```sh
juicyfruit task ready
juicyfruit task show <task>
juicyfruit task claim <task> --adapter <adapter> --session <session>
juicyfruit task checkpoint <task>
juicyfruit task resume <task>
juicyfruit context build <task>
juicyfruit template match <kind>
```
