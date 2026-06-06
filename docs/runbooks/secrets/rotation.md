# Rotate a secret

## When to use this

Routine rotation (quarterly), or after a suspected leak. Both cases run the same procedure; suspected leak just compresses the schedule.

## What it does

The `rotate-secret` subcommand stages a new value in the configured backend, calls a per-secret verifier callback, then commits. On verifier failure the old value is restored & the subcommand exits non-zero without ever touching the live binary's view. On success the old value is returned to stderr so the operator can hold it for emergency manual restore.

Every rotation writes an `audit_log` row (start + completion) & posts a tier-1 ntfy to `grimnir-audit-<region>`.

## Procedure

1. Pick the backend. `GRIMNIR_SECRETS_BACKEND=env` reads `.env`; `=vault` reads HashiCorp Vault KV v2.
2. Run the rotation:

   ```bash
   grimnir-deploy rotate-secret --name NTFY_TOKEN_PAGE --new-value "$(openssl rand -hex 32)"
   ```

3. Confirm the audit notification arrived on `grimnir-audit-<region>`.
4. Confirm Prometheus alerts still fire end-to-end: trigger a test alert (or wait for the next real one) & verify the page arrives on `grimnir-region-<region>-page`. If the token is broken, the bridge logs a 401 from ntfy.
5. The old value is printed to stderr in step 2. Stash it for 24h in case of silent failure; then discard.

## After a leak

Same procedure, but:

- Rotate every credential the leak might cover, not just the one you're sure about. Vault makes this cheap; `.env` requires a quick `grep` for which paths used the credential.
- File a security incident: `docs/runbooks/security-incident.md`.
- Audit who accessed the secret store in the leak window. For `.env`, that's filesystem ACLs + syslog. For Vault, that's `vault audit list` plus the file/syslog audit device.

## What can go wrong

- Verifier fails → old value restored, subcommand exits non-zero, no audit-completion row (only the start row). Operator investigates why the verifier failed; usually the new value is malformed (wrong length, wrong charset).
- ntfy publish fails → rotation still committed; `audit_log` row written. The bridge's retry budget is three attempts (200ms / 500ms / 1500ms). Persistent failure logs at ERROR & shows up on the Grafana audit dashboard as missing rows.
- Vault unreachable → `rotate-secret` exits non-zero before touching anything. Switch to `env` backend (operator decision; not automatic).
