# Security policy

docker-sops handles secrets, so please report vulnerabilities privately.

## Reporting

Use GitHub's private reporting form:
https://github.com/mohsen0/docker-sops/security/advisories/new

Do not open a public issue for anything that could expose plaintext secrets,
key material, or let one user read another's files. Include the plugin
version (`docker sops version`), your OS, and steps to reproduce.

You should get an acknowledgement within a week. Fixes ship as a new release
and are credited in the advisory unless you prefer otherwise.

## Supported versions

Only the latest release receives fixes.

## Scope notes

- The plugin decrypts with the sops library. Weaknesses in sops, age, or a
  cloud KMS belong to those projects.
- Decrypted files live in a private temp directory only while the wrapped
  command runs. Anything that reads them before cleanup, or leaves them
  behind, is in scope.
