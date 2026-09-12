# Storing your sops key in the OS keychain

Most people run sops with an [age](https://age-encryption.org) key sitting in
a plaintext file such as `~/.config/sops/age/keys.txt`. That works, but the
file is readable by any process running as you. Each desktop OS has a native
credential store that encrypts secrets at rest and gates access behind your
login session, and sops can pull the key from it on demand.

The hook is `SOPS_AGE_KEY_CMD`. When set, sops (and therefore `docker sops`)
runs that command and reads age identities from its stdout. The command
string is split shell-style but executed directly, without a shell, so keep
it to one executable and its arguments. sops also exports
`SOPS_AGE_RECIPIENT` into the command's environment, which lets a smarter
helper pick the right identity for the file being decrypted.

This page shows the setup for each OS. Users of cloud KMS, Vault or PGP do
not need any of this; their credentials already live outside plain files.

A native `docker sops key` command backed by the same OS stores is planned
(phase 4 of the development plan) so the steps below become a single
`docker sops key set`. Until then, the environment-variable approach works
with the stock `sops` binary today.

## Generate a key once

```sh
age-keygen
# Public key: age1...
# AGE-SECRET-KEY-1...
```

Copy the `AGE-SECRET-KEY-...` line into the store as shown below, then delete
it from your terminal scrollback and any file you pasted it into. Add the
public key to `.sops.yaml` in your repositories.

## macOS: Keychain

Store the key as a generic password item. The `security` tool is part of
macOS.

```sh
security add-generic-password -a "$USER" -s sops-age-key \
  -w 'AGE-SECRET-KEY-1...' -U
```

Tell sops to read it back:

```sh
# ~/.zshrc or ~/.bashrc
export SOPS_AGE_KEY_CMD='security find-generic-password -s sops-age-key -w'
```

The first time a new build of `security`, `sops` or `docker-sops` reads the
item, Keychain Access asks you to allow it. Choose "Always Allow" to stop
being prompted. To rotate or remove:

```sh
security add-generic-password -a "$USER" -s sops-age-key -w 'AGE-SECRET-KEY-1NEW' -U
security delete-generic-password -s sops-age-key
```

## Linux: Secret Service (GNOME Keyring, KDE Wallet)

Any desktop that implements the freedesktop Secret Service API works. GNOME
Keyring does natively; KDE Plasma 5.x/6.x exposes KWallet through the same
API. Install the CLI client:

```sh
sudo apt install libsecret-tools      # Debian / Ubuntu
sudo dnf install libsecret            # Fedora
sudo pacman -S libsecret              # Arch
```

Store the key (the value is read from stdin so it never appears in `ps`):

```sh
printf '%s' 'AGE-SECRET-KEY-1...' | \
  secret-tool store --label='sops age key' service sops-age-key
```

Read it back:

```sh
# ~/.bashrc, ~/.zshrc or ~/.config/environment.d/sops.conf
export SOPS_AGE_KEY_CMD='secret-tool lookup service sops-age-key'
```

Remove with `secret-tool clear service sops-age-key`.

Headless servers and CI runners usually have no Secret Service daemon. There,
use a password manager CLI with an unlocked agent instead, for example
`SOPS_AGE_KEY_CMD='pass show sops/age-key'` or
`SOPS_AGE_KEY_CMD='gopass show -o sops/age-key'`, or fall back to
`SOPS_AGE_KEY_FILE` on an encrypted volume with `0600` permissions.

## Windows: Credential Manager

Windows stores generic credentials in Credential Manager, protected by DPAPI
under your user account. The built-in `cmdkey` can write credentials but
cannot print a stored password, so reading requires PowerShell with the
`CredentialManager` module.

Install the module once (PowerShell 5.1 or 7):

```powershell
Install-Module -Name CredentialManager -Scope CurrentUser
```

Store the key:

```powershell
New-StoredCredential -Target sops-age-key -UserName age `
  -Password 'AGE-SECRET-KEY-1...' -Persist LocalMachine | Out-Null
```

Create a small helper script, for example
`%USERPROFILE%\bin\sops-age-key.ps1`:

```powershell
(Get-StoredCredential -Target sops-age-key).GetNetworkCredential().Password
```

Point sops at it. Set this as a user environment variable (System Properties,
or `setx`) so it applies to every terminal:

```powershell
setx SOPS_AGE_KEY_CMD 'powershell -NoProfile -ExecutionPolicy Bypass -File C:\Users\you\bin\sops-age-key.ps1'
```

Open a new terminal afterwards; `setx` does not update the current session.
Remove with `Remove-StoredCredential -Target sops-age-key`.

WSL users: WSL runs Linux, so follow the Linux section inside the
distribution, or call `powershell.exe` from `SOPS_AGE_KEY_CMD` to reuse the
Windows credential.

## Verify

```sh
sops -d some-encrypted-file.yaml
docker sops decrypt some-encrypted-file.yaml
```

If sops reports "no identity matched any of the recipients", check that the
public key in `.sops.yaml` matches the private key in the store
(`age-keygen -y` prints the public key for a private one) and that
`SOPS_AGE_KEY_CMD` is exported in the shell that runs docker.

## Notes on safety

- The key is still handed to sops as a process pipe. Anything that can attach
  a debugger to your session can read it. The keychain protects the key at
  rest and from other users, not from you.
- `SOPS_AGE_KEY` (the value itself in the environment) is convenient but
  leaks into child processes and crash dumps. Prefer `SOPS_AGE_KEY_CMD`.
- Keep a backup of the private key somewhere offline. Keychains are tied to
  a login and a machine.
