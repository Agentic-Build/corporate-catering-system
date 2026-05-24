# `ops/secrets/`

SOPS-encrypted Kubernetes Secret manifests for the tbite-platform chart.

The runbook lives in [`docs/deployment/secrets.md`](../../docs/deployment/secrets.md);
this README is the quick reference for operators who already know the flow.

## Layout

```
ops/secrets/
  README.md              # this file
  example.sops.yaml      # template; not encrypted, do not put real values here
  <env>/                 # one directory per environment
    app.sops.yaml        # tbite-app-secrets (DB, NATS, S3, OIDC, Hydra, ...)
    monitoring.sops.yaml # monitoring-secrets (Grafana admin, exporter DSNs)
    ...                  # split however your blast-radius needs require
```

`<env>` is typically one of `single-node/`, `gcp/`, or a customer-specific name.
The convention is "one Secret object per file, named to match its `metadata.name`",
which makes `sops -d ... | kubectl apply -f -` safe to run per file.

Encrypted files keep `apiVersion`, `kind`, `metadata`, and `type` in plaintext
(see `.sops.yaml`'s `encrypted_regex`) so the outer YAML stays kubectl-parseable
and human-greppable.

## Common operations

```bash
# Edit a file in-place with $EDITOR; SOPS decrypts, you save, SOPS re-encrypts.
make sops-edit FILE=ops/secrets/single-node/app.sops.yaml

# Encrypt a freshly-filled-in file (or re-encrypt after manual edit).
make sops-encrypt FILE=ops/secrets/single-node/app.sops.yaml

# Decrypt to stdout (for piping into kubectl, or diffing).
make sops-decrypt FILE=ops/secrets/single-node/app.sops.yaml

# Apply to the current kubectl context.
sops -d ops/secrets/single-node/app.sops.yaml | kubectl apply -f -
```

## Rotation

Rotate the symmetric data key (e.g. after a recipient leaves) without changing
the cleartext payload:

```bash
sops updatekeys ops/secrets/single-node/app.sops.yaml
```

Rotate the cleartext value (e.g. a leaked DB password):

1. `make sops-edit FILE=ops/secrets/<env>/app.sops.yaml`
2. Replace the value, save, commit the new ciphertext.
3. Apply the new Secret (`sops -d ... | kubectl apply -f -`).
4. Restart the consuming workloads so they pick the new env value
   (`kubectl -n tbite rollout restart deploy/api` etc.).

## Adding a recipient

1. New operator: `age-keygen -o ~/.config/sops/age/keys.txt` and share the
   `# public key: age1...` line (NOT the private key) out-of-band.
2. Append that `age1...` value to every `age:` block in `.sops.yaml`.
3. Re-wrap every encrypted file so the new recipient gets a data-key copy:
   ```bash
   find ops/secrets chart -name '*.sops.yaml' -not -name 'example.sops.yaml' \
     -exec sops updatekeys -y {} \;
   ```
4. Commit. Removing a recipient is the same flow with their line deleted;
   treat any value previously held in the affected files as compromised and
   rotate it.

## What does NOT live here

- Plaintext dev credentials for `make dev` — those are in `.env.example` and
  `ops/local/`.
- The single-node demo bootstrap (`ops/kubernetes/overlays/single-node/secrets-bootstrap.yaml`)
  — that file is *intentionally* plaintext and labelled as throwaway; replace
  it with a real `ops/secrets/single-node/app.sops.yaml` before exposing the
  cluster to anything that matters.
- External-secrets / Vault / cloud KMS bindings — those are optional BYO
  add-ons, see `docs/deployment/secrets.md`.
