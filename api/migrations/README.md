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

The runner is introduced with the first migration that needs it (Phase 1 sets up
index management).
