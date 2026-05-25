# Backup & restore — data plane

Operational runbook for the stateful components, and the restore-drill
procedure. Fills the "restore documentation" / "backup and restore
drills" acceptance items of [`adr-0004`](../architecture/adr-0004-self-hosted-ha-data-plane.md)
(#51) and [`adr-0007`](../architecture/adr-0007-postgres-connection-and-backup.md)
(#54).

Recovery objectives (#54): **RPO ≤ 5 min, RTO ≤ 30 min** for Postgres.

## Postgres (CloudNativePG) — authoritative state

The CNPG `Cluster` (`chart/tbite-platform/templates/cnpg-cluster.yaml`)
streams base backups and WAL to an S3-compatible object store via
Barman when `postgres.cluster.backup.enabled=true`:

- `barmanObjectStore.destinationPath` + `s3CredentialsSecretRef`
  (`tbite-pg-backup-s3`, keys `ACCESS_KEY_ID` / `ACCESS_SECRET_KEY`).
- WAL and data are gzip-compressed; `retentionPolicy` prunes old
  backups.
- A daily base backup runs at 03:00 via the `ScheduledBackup`
  (`cnpg-backup-schedule.yaml`). Continuous WAL archiving is what keeps
  RPO at minutes, not a day.

### Restore (point-in-time or latest)

CNPG restores by **bootstrapping a new Cluster** from the object store —
you do not restore in place. Create a recovery cluster that points at
the same `barmanObjectStore`:

```yaml
# recovery-cluster.yaml (apply into the target namespace)
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: tbite-pg-restore
spec:
  instances: 1
  storage: { size: 20Gi }
  bootstrap:
    recovery:
      source: tbite-pg            # references an externalClusters entry
      # for PITR add:
      # recoveryTarget: { targetTime: "2026-05-25 11:00:00+00" }
  externalClusters:
    - name: tbite-pg
      barmanObjectStore:
        destinationPath: <same destinationPath as the backup>
        s3Credentials:
          accessKeyId:     { name: tbite-pg-backup-s3, key: ACCESS_KEY_ID }
          secretAccessKey: { name: tbite-pg-backup-s3, key: ACCESS_SECRET_KEY }
```

Then repoint `DATABASE_RW_URL` / `DATABASE_RO_URL` (the `tbite-db`
Secret) at the recovered cluster's `-rw` / `-ro` services and restart
the app roles. Omit `recoveryTarget` to recover to the latest
archived WAL.

> The chart ships the backup half of this contract; the recovery
> `Cluster` is created by the operator at restore time (it is a
> deliberate, one-off action, not a standing resource).

## Other components

| Component | Holds | Loss impact | Recovery |
| --- | --- | --- | --- |
| Valkey | sessions, cache, rate-limit counters | users re-authenticate; caches repopulate | No backup needed. Redeploy; the app rebuilds cache/read-models from Postgres + the event plane. |
| NATS JetStream | durable domain events (`ORDERS_V1` 30d, `PAYROLL_V1` 90d), DLQ | in-flight/replayable events | FileStorage PVC. For DR, snapshot the PVC (VolumeSnapshotClass) or rely on stream retention + outbox re-publication. Consumers resume from last ack. |
| MinIO | menu images, payroll exports, compliance documents | uploaded artefacts | Tenant erasure coding survives disk loss. For off-site DR, `mc mirror` the buckets to a second target, or snapshot the tenant PVCs. |

## Restore drill (run quarterly, non-prod)

1. Provision a scratch namespace and a recovery `Cluster` from the
   production object store (PITR to a recent timestamp).
2. Confirm the cluster reaches `Cluster in healthy state` and row
   counts on key tables (`platform.outbox`, orders, payroll batches)
   match expectations for the target time.
3. Point a throwaway API replica at the recovered `-rw`/`-ro` and run
   `/readyz` + a read smoke (`GET /api/employee/menu`).
4. Record wall-clock time from "start restore" to "API ready" and
   confirm it is within the 30-min RTO. Tear down the scratch
   namespace.

Dashboards/alerts for pool saturation, replica lag, backup age, and
WAL archiving live in `chart/tbite-platform/templates/vmalert-rules.yaml`;
a stale-backup alert is the early warning that RPO is at risk.
