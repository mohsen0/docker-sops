# Age key in the OS keychain

Instead of keeping the age private key in a file, store it in your OS
credential store and let sops fetch it with `SOPS_AGE_KEY_CMD`. The command
is run without a shell; quote arguments as needed.

## macOS Keychain

```sh
security add-generic-password -a "$USER" -s sops-age-key -w 'AGE-SECRET-KEY-1...' -U
export SOPS_AGE_KEY_CMD='security find-generic-password -s sops-age-key -w'
```

Or let the plugin manage it: `age-keygen | docker sops key set`.

## Linux (GNOME Keyring / KWallet via Secret Service)

```sh
printf '%s' 'AGE-SECRET-KEY-1...' | secret-tool store --label='sops age key' service sops-age-key
export SOPS_AGE_KEY_CMD='secret-tool lookup service sops-age-key'
```

Needs `libsecret-tools` (Debian/Ubuntu) or `libsecret` (Fedora/Arch). Headless
machines: use `pass show sops/age-key` or a `SOPS_AGE_KEY_FILE` with mode 0600.

## Windows Credential Manager

```powershell
Install-Module CredentialManager -Scope CurrentUser
New-StoredCredential -Target sops-age-key -UserName age -Password 'AGE-SECRET-KEY-1...' -Persist LocalMachine
```

Save this as `C:\Users\you\bin\sops-age-key.ps1`:

```powershell
(Get-StoredCredential -Target sops-age-key).GetNetworkCredential().Password
```

```powershell
setx SOPS_AGE_KEY_CMD 'powershell -NoProfile -ExecutionPolicy Bypass -File C:\Users\you\bin\sops-age-key.ps1'
```

Verify on any OS with `docker sops decrypt some-encrypted-file`.
