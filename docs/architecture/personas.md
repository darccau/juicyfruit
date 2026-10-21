# Personas

Personas define voice and communication preferences. They do not replace rules,
skills, agents, or memory:

- `rules/` constrain behavior.
- `skills/` teach task-specific procedures.
- `agents/` define specialist worker roles.
- `personas/` shape tone, phrasing, and durable user-facing preferences.

## On-Disk Shape

Canonical persona seeds live in the source repository:

```text
personas/
├── juicyfruit.md
└── user.md
```

When installed into the generic Juicyfruit target for a project, they appear under
`.juicyfruit/personas/`. `juicyfruit context build` includes Markdown files from that
directory in the `personas` source group.

## Update Semantics

Persona files are seed content. Juicyfruit creates them when absent, then preserves
the installed copy across future package updates. If a package ships a new
default persona, existing local persona files are left untouched.

Use `personas/juicyfruit.md` for Juicyfruit's product voice. Use `personas/user.md`
for the current user's durable communication preferences.
