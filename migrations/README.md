# `migrations/`

Forward-only SQL migrations driven by [golang-migrate]. Each migration
is two files: `<NNNNNN>_<slug>.up.sql` and `<NNNNNN>_<slug>.down.sql`.

The `Dockerfile` here builds the migration image that the Helm chart's
pre-install/pre-upgrade Job runs against the cluster's Postgres.

## Apply

```bash
make migrate-up                  # runs against $DATABASE_URL
make migrate-down                # rolls back one step
make migrate-new name=my_change  # scaffolds the next NNNNNN_*.{up,down}.sql pair
```

## Conventions

- **Forward-only in production.** `down.sql` exists for local rollback
  and tests; never relied on for prod recovery (use the backup/restore
  runbook in [`docs/deployment/backup-restore.md`](../docs/deployment/backup-restore.md)
  instead).
- One logical change per migration. Keep them small so PITR replays
  fast.
- Idempotency is preferred — `CREATE TABLE IF NOT EXISTS`,
  `CREATE INDEX CONCURRENTLY IF NOT EXISTS` (concurrent indexes must be
  in their own migration, no transaction).
- Money columns are whole-NTD integers; name them `*_minor` to signal
  the unit and never divide by 100 anywhere.
- Plant/date is the dominant internal partition key per
  [`adr-0008`](../docs/architecture/adr-0008-single-enterprise-plant-aware-scaling.md).

[golang-migrate]: https://github.com/golang-migrate/migrate
