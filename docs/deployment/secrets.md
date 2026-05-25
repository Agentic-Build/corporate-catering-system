# Secrets: SOPS + age

The tbite-platform repo ships secrets the same way it ships every other
manifest: as files in Git. Cleartext credentials never land on `main`;
instead, each Kubernetes `Secret` is encrypted with [SOPS][sops] using
[age][age] recipients, and decrypted on the way to the cluster (either by
the operator running `kubectl apply`, or by an ArgoCD/kustomize plugin).

This page is the operator runbook. The quick reference for the file layout
lives in [`ops/secrets/README.md`](../../ops/secrets/README.md).

[sops]: https://github.com/getsops/sops
[age]: https://github.com/FiloSottile/age

## Why SOPS + age (and not X)

The platform is designed to be **self-hostable from one box up to a multi-AZ
cluster**, including air-gapped environments (see
[`airgapped.md`](./airgapped.md)). Every secret mechanism we considered was
scored against that constraint:

| Option                          | Verdict for the baseline                                                                                                                                  |
| ------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Plaintext Secret manifests      | Rejected. Cleartext credentials in Git is a non-starter for anything past `make dev`.                                                                     |
| **SOPS + age** (the baseline)   | Chosen. Static binary, no daemon, no extra cluster service, no network egress at decrypt time. Operator's age key sits at `~/.config/sops/age/keys.txt`. |
| SealedSecrets                   | Per-cluster controller, per-cluster keypair, rotation is invasive. Doesn't help when secrets must travel between staging/prod or be re-encrypted for review. |
| External-Secrets + Vault        | Strong, but adds Vault (HA, unseal, audit) as a hard runtime dep. Useful as a BYO add-on in regulated environments, not as the baseline.                  |
| Cloud secret managers (SM/KMS)  | Couples deploys to one cloud and breaks air-gapped installs by definition.                                                                                |
| Helm `--set` from CI            | Pushes secrets into CI env vars and out of source review. Operationally hostile for multi-operator teams.                                                 |

`sops` reads the repo-root [`/.sops.yaml`](../../.sops.yaml) to decide which
age recipients to wrap each file for and which keys inside the YAML to
encrypt. We deliberately leave `apiVersion`, `kind`, `metadata`, and `type`
in cleartext via `encrypted_regex`, so the outer Secret shape stays grep-
and review-friendly, and `kubectl apply -f` on a decrypted stream
works without extra massaging.

## Generating an age key

Every operator with decrypt rights needs their own age keypair. The private
half stays on their workstation; only the `age1…` public half ever lands in
Git.

```bash
mkdir -p ~/.config/sops/age
age-keygen -o ~/.config/sops/age/keys.txt
chmod 600 ~/.config/sops/age/keys.txt
```

The first line of `keys.txt` is a comment of the form
`# public key: age1qzv…`. Paste that `age1…` value into `.sops.yaml` (see
below). SOPS finds the private key automatically because
`~/.config/sops/age/keys.txt` is the default path; override with
`SOPS_AGE_KEY_FILE=…` if you keep it elsewhere (a YubiKey-backed
`age-plugin-yubikey` identity file, for example).

Back up `keys.txt` somewhere offline. Losing it means losing decrypt
access; if you're the only recipient, it means losing every secret in the
repo.

## Adding a team recipient

1. New operator runs `age-keygen` (above) and sends you the `age1…` public
   line out-of-band. Never paste the contents of `keys.txt` itself.
2. Append the new `age1…` to **every** `age:` block in `.sops.yaml`.
   Both the narrow (`ops/secrets/**`) and broad (`chart/**`, `ops/**`)
   rules need it, otherwise the new recipient can read some files but not
   others.
3. Re-wrap every encrypted file so it carries a data-key copy for the new
   recipient:
   ```bash
   find ops/secrets chart -name '*.sops.yaml' -not -name 'example.sops.yaml' \
     -exec sops updatekeys -y {} \;
   ```
   `updatekeys` only rewrites the wrapped data key; the ciphertext payload
   is untouched, so the diff is small and reviewable.
4. Commit `.sops.yaml` and the re-wrapped files in the same PR.

Removing a recipient follows the same flow with their line deleted from
`.sops.yaml`. **Treat the values in every affected file as compromised**
and rotate them — `updatekeys` does not change the plaintext, so a former
operator who held a copy of the old ciphertext + their old key can still
decrypt history.

## Repo layout

```
.sops.yaml                          # recipient + match rules (this is the only config SOPS reads)
ops/secrets/
  README.md
  example.sops.yaml                 # template, intentionally not encrypted
  <env>/
    app.sops.yaml                   # tbite-app-secrets: DB, NATS, S3, OIDC, Hydra
    monitoring.sops.yaml            # monitoring-secrets: Grafana admin, exporter DSNs
chart/tbite-platform/               # umbrella chart; values reference Secret names, not values
```

`<env>` is conventionally `single-node/`, `gcp/`, or a per-customer name.
One Kubernetes Secret per file keeps blast radius small and makes
`sops -d <file> | kubectl apply -f -` a safe per-file operation.

The chart values reference secrets by `secretKeyRef.name` + `key`; the
actual Secret objects come from these SOPS files. That separation is what
lets the chart stay public and the credentials stay private.

## Day-to-day operations

```bash
# Encrypt in place (after filling REPLACE_ME values).
make sops-encrypt FILE=ops/secrets/single-node/app.sops.yaml
# equivalent to: sops -e -i ops/secrets/single-node/app.sops.yaml

# Open the file in $EDITOR; SOPS decrypts, you save, SOPS re-encrypts.
# This is the recommended path for "change one value" edits.
make sops-edit FILE=ops/secrets/single-node/app.sops.yaml
# equivalent to: sops ops/secrets/single-node/app.sops.yaml

# Decrypt to stdout (review, diff, pipe to kubectl).
make sops-decrypt FILE=ops/secrets/single-node/app.sops.yaml
# equivalent to: sops -d ops/secrets/single-node/app.sops.yaml

# Apply the decrypted Secret to the current kubectl context.
sops -d ops/secrets/single-node/app.sops.yaml | kubectl apply -f -

# Re-wrap data key for a new/removed recipient (no cleartext change).
sops updatekeys ops/secrets/single-node/app.sops.yaml
```

After applying a rotated Secret, restart the consumers so they pick up the
new env values:

```bash
kubectl -n tbite rollout restart deploy/api deploy/worker deploy/scheduler
```

## GitOps integration

The umbrella chart and overlays never read the SOPS files directly — they
reference Secrets by name (`secretKeyRef`). Three supported wiring options,
in order of how much we recommend them:

1. **ArgoCD with the `sops` config-management plugin**
   (`argoproj-labs/argocd-vault-plugin` or the `viaduct-ai/kustomize-sops`
   plugin, mounted into the `argocd-repo-server` Pod with the recipient
   age key from a per-cluster Secret). ArgoCD decrypts on sync; the
   cleartext never lands in Git. This is the recommended path for any
   multi-cluster install.
2. **`kustomize-sops` as a generator** inside the kustomize overlay, with
   the operator running `kubectl apply -k …` locally. Good fit for single-
   node installs where ArgoCD isn't worth the extra moving parts.
3. **`sops -d … | kubectl apply -f -`** as a pre-apply step in `make prod-up`
   or an equivalent runbook script. Simplest, requires the operator to have
   the age private key on their workstation at deploy time.

The Makefile only ships the raw SOPS targets; pick (1) or (2) per
environment and wire them in your overlay/Application manifest. See
[`docs/deployment/argocd.md`](./argocd.md) for the ArgoCD bootstrap and
[`docs/deployment/single-node.md`](./single-node.md) for the local-apply
path.

## BYO mode: external-secrets / Vault / cloud KMS

SOPS+age is the **baseline** because it works on a laptop, on a single
box, and in an air-gapped cluster. For regulated environments where the
control of secret material has to live outside the Git workflow, the
platform is happy to coexist with:

- **external-secrets-operator** pulling from HashiCorp Vault, AWS Secrets
  Manager, GCP Secret Manager, or Azure Key Vault and synthesising the
  same Secret objects the chart references. Stop committing the matching
  SOPS file once ESO is the source of truth — keep the rest.
- **HashiCorp Vault** with the agent injector pattern; SOPS files become
  bootstrap-only (Vault root token, unseal shards held offline).
- **Cloud KMS-backed sops** (`sops --kms`, `sops --gcp-kms`,
  `sops --azure-kv`) as an alternative recipient alongside age, for teams
  that want HSM-backed key custody but want to keep the same file layout.

These are add-ons, not replacements: every component still consumes a
`Secret`, and the chart values stay unchanged.
