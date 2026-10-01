#!/bin/sh
# Ferretta release installer. Keep this file POSIX sh compatible.
# The final invocation runs only after the complete function has been parsed.
main() {
    set -eu
    umask 022
    ferretta_repo=https://github.com/ericdmoore/ferretta
    ferretta_version=${FERRETTA_VERSION:-}
    ferretta_dir=${FERRETTA_INSTALL_DIR:-}
    ferretta_no_input=0
    ferretta_ollama=0
    ferretta_litellm=0
    ferretta_model=
    ferretta_tmp=
    ferretta_stage=
    fail() { printf 'ferretta: %s\n' "$*" >&2; exit 1; }
    fetch() { curl --fail --silent --show-error --location --proto '=https' --proto-redir '=https' --tlsv1.2 --connect-timeout 15 --max-time 300 "$@"; }
    cleanup() {
        [ -z "$ferretta_stage" ] || rm -f "$ferretta_stage"
        [ -z "$ferretta_tmp" ] || rm -rf "$ferretta_tmp"
    }
    ask() {
        [ "$ferretta_no_input" = 0 ] && [ "$ferretta_tty" = 1 ] || return 1
        printf '%s [y/N]: ' "$1" >/dev/tty
        IFS= read -r ferretta_answer </dev/tty || return 1
        case "$ferretta_answer" in y|Y|yes|YES) return 0 ;; *) return 1 ;; esac
    }
    while [ "$#" -gt 0 ]; do
        case "$1" in
            --no-input) ferretta_no_input=1 ;;
            --with-ollama) ferretta_ollama=1 ;;
            --with-litellm) ferretta_litellm=1 ;;
            --pull-model)
                [ "$#" -ge 2 ] || fail '--pull-model requires a model name'
                shift; ferretta_model=$1
                case "$ferretta_model" in ''|-*|*[!a-zA-Z0-9._:/-]*) fail 'Invalid model name' ;; esac ;;
            -h|--help)
                cat <<'HELP'
Usage: sh install.sh [--no-input] [--with-ollama] [--with-litellm] [--pull-model NAME]
Installs the latest published stable Ferretta release for macOS/Linux amd64/arm64.
FERRETTA_VERSION=vX.Y.Z selects a published tag (including a prerelease).
FERRETTA_INSTALL_DIR=/absolute/path overrides the destination.
Default: $HOME/.local/bin; root uses /usr/local/bin. No automatic sudo or PATH edits.
Existing regular binaries are atomically replaced; symlinks/directories are refused.
--no-input skips prompts; explicit component flags still perform those actions.
--with-ollama runs Ollama's installer, which may request sudo and start a service.
--with-litellm installs the proxy with an existing uv; it does not start it.
--pull-model explicitly downloads a model through an already running Ollama.
LiteLLM inference and OpenRouter are not yet supported by Ferretta.
HELP
                return 0 ;;
            *) fail "Unknown argument: $1 (use --help)" ;;
        esac
        shift
    done
    for ferretta_command in curl tar awk mktemp uname chmod mv mkdir; do
        command -v "$ferretta_command" >/dev/null 2>&1 || fail "Required command missing: $ferretta_command"
    done
    case "$(uname -s)" in Darwin) ferretta_os=darwin ;; Linux) ferretta_os=linux ;; *) fail 'Supported systems: macOS and Linux' ;; esac
    case "$(uname -m)" in x86_64|amd64) ferretta_arch=amd64 ;; aarch64|arm64) ferretta_arch=arm64 ;; *) fail 'Supported architectures: amd64 and arm64' ;; esac
    # Prefer a native binary when invoked from a translated macOS shell.
    if [ "$ferretta_os" = darwin ] && [ "$(sysctl -n sysctl.proc_translated 2>/dev/null || true)" = 1 ]; then
        ferretta_arch=arm64
    fi
    if command -v sha256sum >/dev/null 2>&1; then ferretta_hash=sha256sum
    elif command -v shasum >/dev/null 2>&1; then ferretta_hash=shasum
    else fail 'Install sha256sum or shasum to verify release checksums'; fi
    if [ -z "$ferretta_dir" ]; then
        if [ "$(id -u)" = 0 ]; then ferretta_dir=/usr/local/bin
        else
            [ -n "${HOME:-}" ] || fail 'HOME is unset; set FERRETTA_INSTALL_DIR'
            ferretta_dir=$HOME/.local/bin
        fi
    fi
    case "$ferretta_dir" in /*) ;; *) fail 'FERRETTA_INSTALL_DIR must be absolute' ;; esac
    # Prompts use the controlling terminal, never the pipe carrying this script.
    ferretta_tty=0
    if [ "$ferretta_no_input" = 0 ] && ( : </dev/tty ) 2>/dev/null; then ferretta_tty=1; fi
    trap cleanup 0
    trap 'exit 130' INT
    trap 'exit 143' HUP TERM
    ferretta_tmp=$(mktemp -d "${TMPDIR:-/tmp}/ferretta-install.XXXXXXXX")
    if [ -z "$ferretta_version" ]; then
        ferretta_latest=$(fetch --output /dev/null --write-out '%{url_effective}' "$ferretta_repo/releases/latest") || fail 'No published stable release is available, or GitHub is unreachable. See https://github.com/ericdmoore/ferretta/releases; FERRETTA_VERSION can select a published prerelease.'
        case "$ferretta_latest" in "$ferretta_repo"/releases/tag/*) ferretta_version=${ferretta_latest##*/} ;; *) fail 'GitHub did not resolve a published stable release' ;; esac
    fi
    case "$ferretta_version" in v[0-9]*) ;; *) fail 'Release version must start with v and a digit' ;; esac
    case "$ferretta_version" in *[!a-zA-Z0-9.+-]*) fail 'Invalid release version' ;; esac
    ferretta_asset=ferretta-$ferretta_version-$ferretta_os-$ferretta_arch.tar.gz
    ferretta_url=$ferretta_repo/releases/download/$ferretta_version
    printf 'Downloading Ferretta %s for %s/%s\n' "$ferretta_version" "$ferretta_os" "$ferretta_arch"
    fetch --output "$ferretta_tmp/archive.tar.gz" "$ferretta_url/$ferretta_asset" || fail 'Release binary unavailable; installation unchanged'
    fetch --output "$ferretta_tmp/checksums.txt" "$ferretta_url/ferretta-$ferretta_version-checksums.txt" || fail 'Release checksums unavailable; installation unchanged'
    ferretta_expected=$(awk -v name="$ferretta_asset" '$2 == name {n++; hash=$1} END {if (n != 1) exit 1; print hash}' "$ferretta_tmp/checksums.txt") || fail 'Missing or duplicate checksum entry'
    [ "${#ferretta_expected}" = 64 ] || fail 'Invalid checksum length'
    case "$ferretta_expected" in *[!0-9a-fA-F]*) fail 'Invalid checksum' ;; esac
    if [ "$ferretta_hash" = sha256sum ]; then ferretta_digest=$(sha256sum "$ferretta_tmp/archive.tar.gz")
    else ferretta_digest=$(shasum -a 256 "$ferretta_tmp/archive.tar.gz"); fi
    ferretta_actual=${ferretta_digest%% *}
    [ "$ferretta_actual" = "$ferretta_expected" ] || fail 'Checksum mismatch; installation unchanged'
    # Extract only the regular binary, never archive paths, links or bundled scripts.
    tar -tzf "$ferretta_tmp/archive.tar.gz" > "$ferretta_tmp/members" || fail 'Invalid release archive'
    [ "$(awk '$0 == "ferretta" {n++} END {print n+0}' "$ferretta_tmp/members")" = 1 ] || fail 'Archive must contain one ferretta binary'
    tar -tvzf "$ferretta_tmp/archive.tar.gz" ferretta > "$ferretta_tmp/type" || fail 'Cannot inspect binary'
    case "$(cat "$ferretta_tmp/type")" in -*) ;; *) fail 'Archive binary is not a regular file' ;; esac
    tar -xOzf "$ferretta_tmp/archive.tar.gz" ferretta > "$ferretta_tmp/ferretta" || fail 'Cannot extract binary'
    [ -s "$ferretta_tmp/ferretta" ] || fail 'Release binary is empty'
    mkdir -p "$ferretta_dir" || fail 'Cannot create install directory; choose FERRETTA_INSTALL_DIR'
    [ -w "$ferretta_dir" ] || fail 'Install directory is not writable; choose FERRETTA_INSTALL_DIR'
    ferretta_target=$ferretta_dir/ferretta
    if [ -L "$ferretta_target" ] || { [ -e "$ferretta_target" ] && [ ! -f "$ferretta_target" ]; }; then
        fail 'Destination is a link or non-regular file; choose another install directory'
    fi
    ferretta_stage=$(mktemp "$ferretta_dir/.ferretta.XXXXXXXX")
    cat "$ferretta_tmp/ferretta" > "$ferretta_stage"
    chmod 755 "$ferretta_stage"
    mv -f "$ferretta_stage" "$ferretta_target"
    ferretta_stage=
    printf 'Installed %s\n' "$ferretta_target"
    case ":$PATH:" in *":$ferretta_dir:"*) ;; *) printf 'Add this directory to your shell PATH: %s\n' "$ferretta_dir" ;; esac
    if [ "$ferretta_ollama" = 1 ] || ask 'Install Ollama using its official installer? It may request sudo and start a service.'; then
        if command -v ollama >/dev/null 2>&1; then printf 'Ollama is already on PATH; leaving it unchanged.\n'
        else
            fetch --output "$ferretta_tmp/ollama-install.sh" https://ollama.com/install.sh || fail 'Ferretta installed; Ollama installer download failed'
            sh "$ferretta_tmp/ollama-install.sh" || fail 'Ferretta installed; Ollama setup failed'
        fi
    fi
    if [ -z "$ferretta_model" ] && command -v ollama >/dev/null 2>&1 && ask 'Download qwen3:4b-thinking (approximately 2.5 GB) through your running Ollama?'; then
        ferretta_model=qwen3:4b-thinking
    fi
    if [ -n "$ferretta_model" ]; then
        command -v ollama >/dev/null 2>&1 || fail 'Ferretta installed; model download requires Ollama on PATH'
        ollama pull "$ferretta_model" || fail 'Ferretta installed; model download failed. Start Ollama and retry the pull explicitly.'
    fi
    if [ "$ferretta_litellm" = 1 ] || ask 'Install LiteLLM for other workflows? Ferretta currently discovers it but cannot review through it. Requires uv.'; then
        command -v uv >/dev/null 2>&1 || fail 'Ferretta installed; install uv first: https://docs.astral.sh/uv/getting-started/installation/'
        uv tool install 'litellm[proxy]' || fail 'Ferretta installed; LiteLLM installation failed'
        printf 'LiteLLM installed but not started or configured. Guide: https://docs.litellm.ai/docs/proxy/quick_start\n'
    fi
    if ask 'Show OpenRouter key setup information? Ferretta does not support this route yet.'; then
        printf 'OpenRouter: https://openrouter.ai/settings/keys\nKeep keys private. Ferretta does not collect or store an unused key. No model or paid fallback is enabled.\n'
    fi
    printf '\nNext: ferretta demo\nThen, from a trusted checkout: ferretta init\nGitHub App: ferretta auth github --setup\nGuide: https://ferretta.cc/docs/getting-started/\n'
}
main "$@"
