# docker sops

A Docker CLI plugin that lets `docker` commands consume
[sops](https://github.com/getsops/sops)-encrypted files without decrypting
them by hand. Keep secrets encrypted in git; decrypt them only for the moment
a command needs them; leave no plaintext behind. Think
[helm-secrets](https://github.com/jkroepke/helm-secrets), for Docker.

> Status: pre-release. The plugin skeleton installs and is discovered by
> Docker; the decrypt and wrapper features are being built. See
> [the development plan](docs/plans/2026-09-12-docker-sops-plugin-development-plan.md).

```sh
docker sops run --env-file secrets.enc.env myimage
docker sops compose -f compose.yaml --env-file .env.enc up -d
docker sops build --secret id=npmrc,src=npmrc.enc .
docker sops decrypt secrets.enc.yaml
```

## Install

From source (needs Go 1.27+):

```sh
git clone https://github.com/mohsen0/docker-sops
cd docker-sops
make install          # copies bin/docker-sops to ~/.docker/cli-plugins/
docker sops version
```

Release binaries, a curl installer and a Homebrew tap arrive with v1.0.0.

## How it works

`docker sops <command> ...` scans the arguments for paths to files that carry
sops metadata, decrypts each one into a private temp directory, swaps the path,
and re-executes the real `docker` CLI. Plain files pass through untouched. For
`compose`, encrypted `env_file:` and `secrets.<name>.file:` entries inside the
project are redirected through a generated override file; your compose files
are never modified. Temp files are removed when the command exits.

Keys are sops' business: `SOPS_AGE_KEY_FILE`, `SOPS_AGE_KEY_CMD`, AWS KMS, GCP
KMS, Azure Key Vault, HashiCorp Vault, PGP and `.sops.yaml` all work exactly as
with the `sops` binary. To keep your age key in the OS keychain instead of a
file, see [docs/keychain.md](docs/keychain.md).

## Documentation

- [Design](docs/specs/2026-09-12-docker-sops-plugin-design.md)
- [Development plan](docs/plans/2026-09-12-docker-sops-plugin-development-plan.md)
- [Storing keys in the OS keychain](docs/keychain.md)

## Development

```sh
make build      # bin/docker-sops
make test       # unit tests (uses the committed age test key)
make lint       # golangci-lint
make fixtures   # re-encrypt testdata/plain/* into testdata/enc/* (needs sops)
make e2e        # installs the plugin and runs tests against a live daemon
```

The age key in `testdata/age-test-key.txt` exists only to decrypt test
fixtures. Never use it for anything else.

## License

Apache-2.0. See [LICENSE](LICENSE) and [NOTICE](NOTICE). This project is not
affiliated with Docker, Inc. or the sops project.
