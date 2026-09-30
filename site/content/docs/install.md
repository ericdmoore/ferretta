---
title: Install Ferretta
description: One binary for macOS or Linux. Choose the components you need.
weight: 1
---
The release installer supports macOS and Linux on **amd64** and **arm64**, including
Apple Silicon. It downloads an archive from Ferretta's GitHub releases, verifies
its SHA-256 checksum, and installs the binary.

```sh
curl -fsSL https://ferretta.cc/install.sh | sh
```

**Launch prerequisite:** this URL becomes available after the site is deployed.
The default installation also needs a published stable release. Draft releases
are private and cannot be installed; a published prerelease needs an explicit tag.

To inspect the script before running it:

```sh
curl -fsSL https://ferretta.cc/install.sh -o install.sh
less install.sh
sh install.sh
```

## Where it installs

Regular users get `$HOME/.local/bin/ferretta` on either OS. Running as root uses
`/usr/local/bin/ferretta`. The installer never invokes sudo for Ferretta itself
and does not edit shell profiles. If the destination is not on PATH, it tells you
which directory to add. Run `ferretta demo` after opening a shell with that PATH.

Override the destination or select a published version:

```sh
FERRETTA_INSTALL_DIR="$HOME/bin" sh install.sh --no-input
FERRETTA_VERSION=vX.Y.Z sh install.sh --no-input
```

Replace `vX.Y.Z` with an actual published tag from
[GitHub releases](https://github.com/ericdmoore/ferretta/releases). A prerelease can
be selected the same way. The default always follows GitHub's latest stable release.
On Apple Silicon, a shell running under Rosetta still selects arm64.

Rerunning installs the selected version over an existing regular binary. Downloads
and checksum verification finish before replacing it, and replacement uses a
rename in the destination directory. Symlink and directory destinations are
refused. Package-managed installations should be updated through their package
manager. No background Ferretta service is started or reconfigured by this script.

Checksums detect corrupted or mismatched downloads; they are served by the same
release source and are not an independent publisher signature. GitHub and the
HTTPS connection remain trusted. Requirements are `curl`, `tar`, a SHA-256 tool
(`sha256sum` or `shasum`), and standard shell utilities.

## Optional components

When a terminal is available, the installer offers each optional action with a
default of **No**. Piped invocation reads answers from the controlling terminal.
Without a terminal it installs only Ferretta. `--no-input` skips all questions;
explicit component flags still authorize the selected actions.

- **Ollama:** supported for Ferretta reviews. The official installer may ask for
  administrator privileges and start its service. An existing `ollama` command
  is left unchanged. [Ollama installation](https://ollama.com/download).
- **Qwen 4B:** an optional, approximately 2.5 GB model download through a running
  Ollama. The installer does not generate model responses. Hardware needs grow
  with context size; passing a tool test does not establish review quality.
- **LiteLLM:** an optional proxy installation using an existing
  [uv](https://docs.astral.sh/uv/getting-started/installation/):
  `uv tool install 'litellm[proxy]'`. It is not started or configured. Ferretta can
  discover its inventory but **cannot review through it yet**.
  [LiteLLM's setup guide](https://docs.litellm.ai/docs/proxy/quick_start).
- **OpenRouter:** the installer can show the
  [key management page](https://openrouter.ai/settings/keys). Ferretta's OpenRouter
  adapter is not implemented, so it does not ask for, store, or spend with a key.
  No hosted or paid fallback is enabled.

Explicit optional setup is also available:

```sh
sh install.sh --no-input --with-ollama
# Once Ollama is running:
sh install.sh --no-input --pull-model qwen3:4b-thinking
# For other workflows; requires uv already installed:
sh install.sh --no-input --with-litellm
```

If an optional component fails, Ferretta stays installed and the script returns
an error identifying the failed component. Fix that component before trying it
again; no automatic retry runs third-party installers or model pulls.

## Build from source

Until a suitable release is published, use Go 1.26.5 and Make:

```sh
git clone https://github.com/ericdmoore/ferretta.git
cd ferretta
make build
bin/ferretta demo
```

Continue with [your first review](../getting-started/), connect a
[GitHub App](../github-app-auth/), then consider the
[background intake service](../service/).
