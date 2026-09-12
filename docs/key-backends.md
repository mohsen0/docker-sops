# Key backends: 1Password, AWS KMS and others

`docker sops` decrypts with the sops library, so it finds keys exactly the
way the `sops` binary does. Which backend is used is decided by the file:
its metadata lists the recipients it was encrypted to (`age`, `kms`,
`gcp_kms`, `azure_kv`, `hc_vault`, `pgp`), and sops tries only those. A file
encrypted to AWS KMS never asks for an age key, and the other way round.

This page shows how to supply credentials for the common setups. The first
two sections are the ones people ask about most.

## age key kept in 1Password

Store the private key once, then let sops fetch it on demand through
`SOPS_AGE_KEY_CMD`. Nothing is written to disk.

1. Generate a key and store it. `age-keygen` prints a public key line and an
   `AGE-SECRET-KEY-1...` line; save the secret line as a password-type item:

   ```sh
   age-keygen -o /dev/stdout 2>/dev/null | grep '^AGE-SECRET-KEY' | \
     op item create --category password --title "myproject sops age key" \
       --vault Personal 'password[password]=-'
   ```

   If you prefer the app: create a Password item, paste the secret line in
   the password field. Any field name works; the examples below use the
   default `password` field. An item created earlier with a custom field
   such as `credential` works the same way, just change the field name.

2. Put the public key in `.sops.yaml`:

   ```yaml
   creation_rules:
     - age: age1...yourpublickey
   ```

3. Tell sops how to read the private key. In `~/.zshrc` or `~/.bashrc`:

   ```sh
   export SOPS_AGE_KEY_CMD='op read "op://Personal/myproject sops age key/password"'
   ```

   `SOPS_AGE_KEY_CMD` is split shell-style and executed directly, without a
   shell, so quote the item name and keep spaces literal (no `%20`).
   The equivalent long form is
   `op item get "myproject sops age key" --vault Personal --fields password --reveal`.

4. Check it:

   ```sh
   docker sops decrypt secrets.env
   ```

Notes:

- With the 1Password desktop app integration enabled, `op` unlocks with
  Touch ID or the system prompt. On a headless machine use a
  [service account](https://developer.1password.com/docs/service-accounts/)
  and export `OP_SERVICE_ACCOUNT_TOKEN`; the same `SOPS_AGE_KEY_CMD` then works
  in CI.
- Each decryption runs the command once. On a long `docker sops compose up`
  that is a single prompt at the start.
- The same pattern works for other managers: `bw get password <item>`
  (Bitwarden), `pass show sops/age-key`, `gopass show -o sops/age-key`.

## AWS KMS

With KMS there is no key file to manage: AWS IAM decides who can decrypt.
The plugin uses the AWS SDK's standard credential chain, so anything that
works for the `aws` CLI works here.

1. Create a symmetric KMS key and give the people and roles that need it
   `kms:Decrypt` (and `kms:Encrypt`, `kms:GenerateDataKey` for those who
   encrypt).

2. Reference it in `.sops.yaml`:

   ```yaml
   creation_rules:
     - path_regex: secrets/prod/.*
       kms: arn:aws:kms:eu-west-1:123456789012:key/11111111-2222-3333-4444-555555555555
     - path_regex: .*
       kms: arn:aws:kms:eu-west-1:123456789012:key/11111111-2222-3333-4444-555555555555
       age: age1...developer-key            # optional: offline fallback
   ```

   To assume a role for KMS calls add it to the ARN: `arn:...:key/...+arn:aws:iam::123456789012:role/sops`. To pin a profile use
   `aws_profile: myprofile` on the rule.

3. Make credentials available in the shell that runs `docker sops`:

   ```sh
   aws sso login --profile myprofile && export AWS_PROFILE=myprofile
   # or: export AWS_ACCESS_KEY_ID=... AWS_SECRET_ACCESS_KEY=... AWS_SESSION_TOKEN=...
   # or: run on an EC2 instance / ECS task with an attached role
   ```

   Credentials are needed by the plugin process on the host, not inside the
   container it starts.

4. Check it:

   ```sh
   docker sops decrypt secrets/prod/app.env
   ```

If credentials are missing the error names the KMS key and the AWS failure
(for example `NoCredentialProviders` or `AccessDeniedException`). It never
falls back to asking for an age key unless the file also lists age
recipients.

CI on GitHub Actions: use OIDC instead of long-lived keys.

```yaml
permissions:
  id-token: write
  contents: read
steps:
  - uses: aws-actions/configure-aws-credentials@v4
    with:
      role-to-assume: arn:aws:iam::123456789012:role/github-sops
      aws-region: eu-west-1
  - run: docker sops compose up -d
```

## Other backends

All of these are configured the same way as with the `sops` binary:

| Backend | `.sops.yaml` key | Credentials come from |
|---|---|---|
| GCP KMS | `gcp_kms: projects/.../cryptoKeys/...` | `gcloud auth application-default login`, `GOOGLE_APPLICATION_CREDENTIALS`, or workload identity |
| Azure Key Vault | `azure_keyvault: https://<vault>.vault.azure.net/keys/<name>/<version>` | `az login`, `AZURE_CLIENT_ID`/`AZURE_CLIENT_SECRET`/`AZURE_TENANT_ID`, or managed identity |
| HashiCorp Vault | `hc_vault_transit_uris: https://vault:8200/v1/transit/keys/sops` | `VAULT_ADDR` and `VAULT_TOKEN` |
| PGP | `pgp: <fingerprint>` | your GnuPG keyring; `gpg-agent` handles the passphrase |
| age, key in a file | `age: age1...` | `SOPS_AGE_KEY_FILE`, or the default `~/.config/sops/age/keys.txt` |
| age, key in the OS keychain | `age: age1...` | `docker sops key set`; see [keychain.md](keychain.md) |

## Mixing backends

A rule may list several backends. Every recipient can decrypt the file
independently, so a common layout is KMS for servers and CI plus each
developer's age key for offline work. After changing the list, run
`sops updatekeys <file>` (or `docker sops encrypt` again) so existing files
pick up the new recipients.
