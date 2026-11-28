# migrations

MongoDB has no schema, but DinnerOS still has **intentional structure**:
indexes, unique constraints, and occasional data backfills.

- **Indexes** are declared next to the repository that relies on them (each
  domain package owns its collection) and applied idempotently at startup via
  `createIndexes`. Creating an index that already exists is a no-op.
- **Data migrations** (backfills, field renames) live in this directory as
  numbered, idempotent Go steps recorded in a `schema_migrations` collection so
  each runs once per database.

Rules:

1. Migrations must be safe to re-run.
2. Never delete user data in a migration without an explicit, reviewed plan.
3. Migrations must not contain personal data or credentials.

`runner.go` runs them in the background when the API starts (`cmd/server`). Each
is claimed in `schema_migrations` (`running`, then `done` or `failed`); a failed
or stalled one runs again on the next start, and a done one never does. Each
logs one line: `migration <id> done: ...`.

| id | what |
|---|---|
| `starter-descriptions-768` | Starter descriptions in DinnerOS's own words (decision 768), after copying `recipes` and `recipe_catalog` into dated backup collections. |
