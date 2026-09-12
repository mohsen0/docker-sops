# Contributing

Thanks for helping. Keep changes small and send a pull request; `main` is
not pushed to directly.

## Setup

Go 1.27+, Docker, and for regenerating fixtures the `sops` binary.

```sh
make build      # bin/docker-sops
make install    # copies it to ~/.docker/cli-plugins/
make test       # unit tests (uses the committed age test key)
make lint       # golangci-lint
make e2e        # runs against your local Docker daemon
make fixtures   # re-encrypts testdata/plain/* into testdata/enc/*
```

## Pull requests

- Write the test first; every package has table tests you can extend.
- Run `make test lint` before pushing. CI also runs e2e on Linux.
- One change per PR. Describe what changed and why in the description;
  it becomes the squash commit message.
- Never commit real keys. `testdata/age-test-key.txt` exists only to decrypt
  the fixtures.

## Where things live

| Path | Purpose |
|---|---|
| `cmd/docker-sops` | CLI commands and the wrapper entry point |
| `internal/argscan` | finds and rewrites file arguments |
| `internal/composefix` | Compose project rewriting |
| `internal/sopsfile` | detection and decryption |
| `internal/tempstore` | private temp directory for decrypted copies |
| `internal/reexec` | re-executes the docker CLI |
| `internal/keychain` | age key in the OS keychain |
| `docs/` | user docs and the design spec |
