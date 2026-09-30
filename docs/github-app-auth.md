# GitHub App authentication

Ferretta's GitHub commands use a dedicated GitHub App installation. API calls
are made directly from Go, and Git fetches receive the same installation's
repository-scoped credentials. There is no fallback to `gh`, `GH_TOKEN`,
`GITHUB_TOKEN`, or Git's personal credential helpers. Offline intent parsing
and help do not require credentials.

This implementation supports github.com, one app/installation per process, and
read-only review and service intake polling. It does not register an app, post
comments, repair code, or merge PRs. Registration remains a browser step.

## Register and install

1. Open [GitHub App registration](https://github.com/settings/apps/new).
2. Choose an available name (for example, `ferretta-ericdmoore`) and use
   `https://github.com/ericdmoore/ferretta` as the homepage.
3. Leave user OAuth callbacks/device authorization unconfigured. Disable the
   webhook for the current polling/manual-review version.
4. Grant repository **Contents: read-only** and **Pull requests: read-only**.
   Metadata read access is supplied by GitHub. No organization or account
   permissions are needed. Select installation on your account only for now.
5. Create the app, copy its **Client ID** (numeric App ID also works), and
   generate a private key. Move the downloaded PEM outside your checkout into
   a private directory. Never commit it or paste its contents into a chat.
6. Choose **Install App**, install on `ericdmoore`, and select only the
   `ferretta` repository. Copy the installation ID from the installation
   settings URL (`.../settings/installations/NUMBER`).

App registration and installation are separate: an app ID is not an
installation ID. A browser user-login token is not an installation token.
GitHub's installation lookup verifies that the configured app/installation
actually belongs to the requested repository before minting a token.

See GitHub's [installation authentication documentation](https://docs.github.com/en/apps/creating-github-apps/authenticating-with-a-github-app/authenticating-as-a-github-app-installation).

## Machine configuration

The default configuration is `ferretta/github.json` beneath Go's
`os.UserConfigDir()`: on macOS, `$HOME/Library/Application Support/ferretta/github.json`;
on Linux, `$XDG_CONFIG_HOME/ferretta/github.json` or `$HOME/.config/ferretta/github.json`.
An absolute `FERRETTA_GITHUB_CONFIG` path overrides this location. An invalid
explicit file is an error, never a reason to try personal credentials.

For alpaca, create the private directory:

```sh
mkdir -p "$HOME/Library/Application Support/ferretta"
chmod 700 "$HOME/Library/Application Support/ferretta"
```

Place the PEM there as `github-app.pem`, then restrict it:

```sh
chmod 600 "$HOME/Library/Application Support/ferretta/github-app.pem"
```

Prefer the guided command, which verifies access before creating configuration:

```sh
bin/ferretta auth github --setup
bin/ferretta auth github --configure --repo ericdmoore/ferretta \
  --client-id YOUR_APP_CLIENT_ID --installation-id 12345678 \
  --private-key-file "$HOME/Library/Application Support/ferretta/github-app.pem"
```

Existing files are never overwritten. For another account, substitute your own
repository and install the App there. When provisioning a boot service, set
`FERRETTA_GITHUB_CONFIG` to its absolute service-owned path and ensure the key is
readable by that service account. Setup does not start the service.

Alternatively create `github.json` manually with the actual IDs:

```json
{
  "client_id": "YOUR_APP_CLIENT_ID",
  "installation_id": 12345678,
  "private_key_file": "/Users/alpaca/Library/Application Support/ferretta/github-app.pem"
}
```

The JSON contains a key-file reference, not a key or installation token. Unknown
fields and trailing JSON values are rejected. Key paths must be absolute;
regular key files must have no group/other permissions. PKCS#1 and PKCS#8 RSA
PEM keys of at least 2048 bits are supported. Encrypted keys are not supported.
Connection credentials are machine configuration, not PR-controlled repository
policy. Keep both files outside repositories and outside exported session data.

## Verify and review

```sh
make build
bin/ferretta auth github --repo ericdmoore/ferretta
bin/ferretta review --repo ericdmoore/ferretta --pr 1
```

`auth github` prints only app ID, installation ID, bot login, and repository.
It checks the installation, obtains a read-only repository-scoped token, and
verifies repository access. It does not generate model responses or write PR
comments. The review command still requires its separate trusted Ollama policy,
an available local model, and a checkout with the matching `origin`.

`intent inspect` uses this same connection. Supply the app's reported bot login
in `--agents` and separate human logins in `--humans`. Bot-authored confirmation
markers are rejected by the existing intent policy. Authentication proves the
posting account; it does not prove a human personally typed a comment when
another program has access to that human's credentials.

## Credential lifetime and effect boundaries

The app signs RS256 JWTs using an injected clock, backdating issuance by one
minute and expiring them nine minutes after the current time. Each installation
token requests only the target repository and Contents/Pull requests read
permissions, even if the installation later gains broader permissions. Tokens
are cached in memory per repository and renewed when at most one minute remains.
They are never written to reports, checkpoints, Git configuration, or config files.
Concurrent requests share token creation. API redirects are refused. Failures
surface without automatic retry or fallback; provider response bodies and
credential-exchange transport details are not included in errors.

Git receives its token through a per-process, URL-scoped authorization header.
No token appears in command arguments or remote URLs. Credential helpers,
interactive prompting, redirects, inherited Git tracing/configuration variables,
and fetch hooks are disabled for that operation. Fetch diagnostics are suppressed
to keep credentials out of reports. Local repository Git configuration remains
trusted operator configuration.

Policy checks receive an environment with known GitHub credentials, connection
references, Git overrides, and SSH agent variables removed. **This is not a
security sandbox.** Checks run as the local OS user and can still access that
user's files or credential stores. Use trusted checks/code for this manual
version; running untrusted repairs/checks with strong separation from human
credentials requires a separate OS account or a sandbox, which is not implemented.

Permission failures, revoked/suspended installations, and unexpected token grants
fail closed. No daemon, comment posting, or model spending starts merely because
a connection file exists. Future write effects must request explicit permissions
and retain core authorization; these read tokens intentionally cannot post or merge.

## Validation

`make check` uses fake HTTP transports and processes, with no live provider calls.
It covers signed JWT verification, renewal, concurrent requests, installation
mismatches/suspension, insufficient/excess permissions, redirects, invalid key
files, and credential handling at the process boundary.

After registering an app, run the separate read-only adapter suite against an
existing PR with comments:

```sh
FERRETTA_TEST_REPO=ericdmoore/ferretta FERRETTA_TEST_PR=1 make test-network-app
```

This suite uses the machine app configuration, verifies identity/access, fetches
PR metadata and comments, and never posts. Token issuance is its only POST.
The older `make test-network` fixture still supports anonymous/PAT-based API
reads; that test-only path is not a CLI authentication fallback.
