# docker sops

A Docker CLI plugin that lets `docker` commands use
[sops](https://github.com/getsops/sops)-encrypted files without decrypting them
by hand. Secrets stay encrypted in git and are decrypted only while a command
runs. Like [helm-secrets](https://github.com/jkroepke/helm-secrets), for Docker.

```sh
docker sops run --rm --env-file secrets.enc.env myimage
docker sops compose up -d
docker sops build --secret id=npmrc,src=npmrc.enc .
docker sops decrypt secrets.enc.yaml
```

## Install

Script (macOS and Linux):

```sh
curl -fsSL https://raw.githubusercontent.com/mohsen0/docker-sops/main/install.sh | sh
```

Homebrew:

```sh
brew tap mohsen0/docker-sops https://github.com/mohsen0/docker-sops
brew install --cask docker-sops
```

Manual: download the binary for your platform from the
[releases page](https://github.com/mohsen0/docker-sops/releases), then

```sh
chmod +x docker-sops_*_darwin_arm64
mkdir -p ~/.docker/cli-plugins
mv docker-sops_*_darwin_arm64 ~/.docker/cli-plugins/docker-sops
```

From source: `make install` (needs Go 1.27+).

## How it works

Prefix a docker command with `sops`. Any argument that is a sops-encrypted
file is replaced by a decrypted copy in a private temp dir, the real `docker`
runs, and the copy is deleted when it exits. Plain files pass through
untouched. Files are recognised by their sops metadata, not their names.

For `compose`, encrypted `env_file:` entries and `secrets:`/`configs:` files
inside the project are handled too, through a generated override file. Your
compose files are never modified.

## Commands

| Command | Purpose |
|---|---|
| `docker sops [OPTIONS] COMMAND [ARGS...]` | Run any docker command with encrypted files decrypted on the fly. |
| `docker sops decrypt [-i] [-o FILE] FILE` | Print or write the plaintext of a file. |
| `docker sops encrypt ...`, `docker sops edit ...` | Pass-through to the `sops` binary. |
| `docker sops key set\|show\|rm` | Manage an age key in the OS keychain. |
| `docker sops version` | Print the version. |

Options before the docker command: `-q/--quiet`, `--tmpdir DIR`,
`--pattern GLOB`, `--no-detect`, `--dry-run`. Run `docker sops --help`.

## Keys

Keys work exactly as with the `sops` binary: age, AWS KMS, GCP KMS, Azure Key
Vault, Vault, PGP and `.sops.yaml`. Decrypting does not need `sops` installed.

- [Age key in the OS keychain](docs/keychain.md)
- [1Password, AWS KMS and other backends](docs/key-backends.md)

## License

Apache-2.0. Not affiliated with Docker, Inc. or the sops project.
