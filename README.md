# docker sops

A Docker CLI plugin that lets `docker` commands consume
[sops](https://github.com/getsops/sops)-encrypted files without decrypting
them by hand. Keep secrets encrypted in git; decrypt them only for the moment
a command needs them; leave no plaintext behind. Think
[helm-secrets](https://github.com/jkroepke/helm-secrets), for Docker.

> Status: pre-release, feature complete for v1.0.0. See
> [the development plan](docs/plans/2026-09-12-docker-sops-plugin-development-plan.md).

```sh
docker sops run --env-file secrets.enc.env myimage
docker sops compose -f compose.yaml --env-file .env.enc up -d
docker sops build --secret id=npmrc,src=npmrc.enc .
docker sops decrypt secrets.enc.yaml
```

## Install

### Release binaries

```sh
curl -fsSL https://raw.githubusercontent.com/mohsen0/docker-sops/main/install.sh | sh
```

Detects your OS/arch, downloads the matching release archive, verifies its
checksum, and installs `docker-sops` into `~/.docker/cli-plugins/`. Set
`DOCKER_SOPS_VERSION` to pin a specific tag, or `DOCKER_CLI_PLUGIN_DIR` to
install elsewhere (e.g. `/usr/local/lib/docker/cli-plugins`).

### Homebrew

```sh
brew install mohsen0/tap/docker-sops
mkdir -p ~/.docker/cli-plugins && ln -sfn "$(brew --prefix)/share/docker-sops/docker-sops" ~/.docker/cli-plugins/docker-sops
```

Homebrew formulas cannot write outside their own prefix, so the symlink step
is manual (the formula prints the same instructions as a caveat after
install).

### Manual

Download the archive for your OS/arch from the
[releases page](https://github.com/mohsen0/docker-sops/releases), verify it
against the accompanying `checksums.txt`, then:

```sh
tar -xzf docker-sops_<version>_<os>_<arch>.tar.gz docker-sops
mkdir -p ~/.docker/cli-plugins
cp docker-sops ~/.docker/cli-plugins/docker-sops
chmod +x ~/.docker/cli-plugins/docker-sops
```

### From source

Needs Go 1.27+:

```sh
git clone https://github.com/mohsen0/docker-sops
cd docker-sops
make install          # copies bin/docker-sops to ~/.docker/cli-plugins/
docker sops version
```

## How it works

`docker sops <command> ...` scans the arguments for paths to files that carry
sops metadata, decrypts each one into a private temp directory (mode 0700,
files 0600), swaps the path, and re-executes the real `docker` CLI with the
same global flags (`--context`, `--host`, ...). Plain files pass through
untouched, so the wrapper is safe on commands that mix encrypted and plain
inputs. Temp files are removed when the command exits, also on Ctrl-C. The
child's exit code is mirrored.

Which files count as encrypted is decided by content, not by name: a top-level
`sops` block with a `mac` (YAML, JSON, binary), a `sops_mac=` line (dotenv) or
a `[sops]` section (INI). Use `--pattern` to add name-based matching.

```sh
docker sops run --rm --env-file secrets.enc.env myimage
docker sops build --secret id=npmrc,src=npmrc.enc .
docker sops secret create db_password db_password.enc     # swarm
docker sops stack deploy -c stack.enc.yaml mystack
docker sops --dry-run run --env-file secrets.enc.env myimage
# docker run --env-file <decrypted:secrets.enc.env> myimage
```

### Compose

For `docker sops compose ...` the plugin also looks inside the project. It
loads the Compose model exactly as Compose would (same `-f`, `--env-file`,
`--profile`, `-p`, `--project-directory`) and generates an override file that
is appended as an extra `-f`. Your compose files are never modified.

- `env_file:` entries that are encrypted are replaced with decrypted temp
  copies. Compose reads them at container creation, so the copies can be
  removed afterwards.
- `secrets:` and `configs:` with an encrypted `file:` are converted to
  `environment:` entries whose value is injected only into the environment of
  the `docker compose` child process. Compose copies that content into the
  container, so nothing is bind-mounted from a temp path and `up -d` keeps
  working after the plugin exits. Binary or very large content (over 64 KiB)
  falls back to a temp file with a warning.

```sh
docker sops compose up -d
docker sops compose -f compose.yaml -f compose.prod.yaml --env-file .env.enc up -d
docker sops --dry-run compose up
# docker compose -f /abs/compose.yaml -f <decrypted:docker-sops.override.yaml> up
```

## Commands

| Command | Purpose |
|---|---|
| `docker sops [OPTIONS] COMMAND [ARGS...]` | Wrapper mode: run any docker command with encrypted files decrypted on the fly. |
| `docker sops decrypt [-i] [-o FILE] FILE` | Print (or write) the plaintext of one file. |
| `docker sops encrypt ...`, `docker sops edit ...` | Pass-through to the `sops` binary (`DOCKER_SOPS_BIN` overrides the path). |
| `docker sops key set [--file F]` | Store an age private key in the OS keychain (stdin or file). |
| `docker sops key show [--private]` | Print the stored key's public key, or the private key for backup. |
| `docker sops key rm` | Remove the stored key. |
| `docker sops version` | Print the plugin version. |

Wrapper options go before the docker command:

| Flag | Env var | Meaning |
|---|---|---|
| `-q`, `--quiet` | `DOCKER_SOPS_QUIET` | Suppress the "decrypted N file(s)" notice. |
| `--tmpdir DIR` | `DOCKER_SOPS_TMPDIR` | Where decrypted copies live (default: OS temp dir). |
| `--no-detect` | `DOCKER_SOPS_NO_DETECT` | Disable content detection; only `--pattern` matches count. |
| `--pattern GLOB` | `DOCKER_SOPS_PATTERN` | Extra basename glob treated as encrypted (repeatable; comma-separated in the env var). |
| `--dry-run` | | Print the rewritten command instead of running it. |

## Keys

Keys are sops' business: `SOPS_AGE_KEY_FILE`, `SOPS_AGE_KEY`, `SOPS_AGE_KEY_CMD`,
AWS KMS, GCP KMS, Azure Key Vault, HashiCorp Vault, PGP and `.sops.yaml` all
work exactly as with the `sops` binary, because the plugin links the same
library. Decryption does not need the `sops` binary installed.

To keep an age key out of plain files, store it in the OS keychain:

```sh
age-keygen | docker sops key set
docker sops key show          # public key, for .sops.yaml
```

When a key is stored and none of `SOPS_AGE_KEY`, `SOPS_AGE_KEY_FILE` or
`SOPS_AGE_KEY_CMD` is set, the plugin uses it. It is never passed to the
wrapped docker process. Manual recipes for macOS Keychain, Linux Secret
Service and Windows Credential Manager are in [docs/keychain.md](docs/keychain.md).

## Comparison with helm-secrets

| | helm-secrets | docker sops |
|---|---|---|
| Integration | `helm secrets <cmd>` wrapper or `secrets://` values URI | `docker sops <cmd>` wrapper (Docker has no URI handler) |
| Decrypt engine | shells out to `sops` (or `vals`) | links the sops library; `sops` binary only for encrypt/edit |
| Detection | file must be a values file passed with `-f` | any argument that is an encrypted file, plus Compose model |
| Plaintext at rest | temp files next to the source or in a temp dir | private temp dir, removed on exit; Compose secrets via environment only |

## Security notes

- Decrypted material is written only to a private temp directory for the
  lifetime of one command, never next to the source file unless you ask
  (`decrypt -i`, `decrypt -o`).
- Decrypted values are never put on the command line, so they do not show up
  in `ps`.
- A keychain-stored age key is exported to this process only and is not
  inherited by the wrapped docker process.
- The plugin does not log file contents at any verbosity.

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
