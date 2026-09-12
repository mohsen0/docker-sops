# Key backends

An encrypted file records which keys it was encrypted to, and sops tries only
those. `.sops.yaml` is used when encrypting; decrypting needs only the
matching credential.

## 1Password (age key)

Store the `AGE-SECRET-KEY-1...` line as a Password item, then:

```sh
export SOPS_AGE_KEY_CMD='op read "op://Personal/my sops age key/password"'
```

Keep spaces literal in the item name. Headless or CI: set
`OP_SERVICE_ACCOUNT_TOKEN`. Same idea for others: `bw get password <item>`,
`pass show sops/age-key`, `gopass show -o sops/age-key`.

## AWS KMS

```yaml
# .sops.yaml
creation_rules:
  - kms: arn:aws:kms:eu-west-1:123456789012:key/11111111-2222-3333-4444-555555555555
```

Credentials come from the normal AWS chain: `aws sso login` with
`AWS_PROFILE`, exported keys, or an instance/task role. They must be present
in the shell that runs `docker sops`, not in the container. Add
`+arn:aws:iam::...:role/name` to the ARN to assume a role, or `aws_profile:`
on the rule to pin a profile.

## Others

| Backend | `.sops.yaml` key | Credentials |
|---|---|---|
| GCP KMS | `gcp_kms: projects/.../cryptoKeys/...` | `gcloud auth application-default login` or workload identity |
| Azure Key Vault | `azure_keyvault: https://<vault>.vault.azure.net/keys/<name>/<version>` | `az login` or managed identity |
| HashiCorp Vault | `hc_vault_transit_uris: https://vault:8200/v1/transit/keys/sops` | `VAULT_ADDR`, `VAULT_TOKEN` |
| PGP | `pgp: <fingerprint>` | GnuPG keyring |
| age (file) | `age: age1...` | `SOPS_AGE_KEY_FILE` or `~/.config/sops/age/keys.txt` |
| age (keychain) | `age: age1...` | [keychain.md](keychain.md) |

A rule may list several backends; any one of them can decrypt. After changing
recipients run `sops updatekeys <file>`.
