# Running Ferretta as a service

`service run` **polls and durably records PR revisions only**. It does
not call a model, run PR code, post comments, repair, or merge. A candidate in
the intake queue is not review admission or approval. Register and provision the
[GitHub App](github-app-auth.md) before enabling polling.

```sh
make build
bin/ferretta service run --repo ericdmoore/ferretta --state /absolute/private/state --once
bin/ferretta service status --state /absolute/private/state
bin/ferretta service run --repo ericdmoore/ferretta --state /absolute/private/state
```

The last command stays in the foreground until SIGINT/SIGTERM. Repeat `--repo`
for multiple repositories. It polls immediately, then waits one minute after
each sweep; `--interval` changes that delay. Each repository read has a two-minute
bound. Failed reads retry on the next sweep; `--once` returns failure if any
repository failed. Database failures stop the process. Configure service-manager
logs to surface polling/authentication failures; this slice has no health API.

The default state location is under the invoking account's config directory,
independent of checkout location. Always pass an explicit path for boot services.
Use one state directory per installation on a local filesystem. Starting a second
owner for that directory fails; separate directories are independent installations
and do not coordinate ownership of a repository.

SQLite stores immutable revision identities (canonical repository, PR number,
head SHA and base SHA), the latest successful listing per repository, and its
current non-draft candidates. Repeated polls and restarts reuse existing identities.
Changed commits retain older observations. Closed/draft PRs disappear from
current candidates after the next successful complete listing; read failures
leave the previous snapshot intact. Transactions prevent partial snapshots.

`service status` reports observation timestamps, current candidates and the total
number of observed revisions. It is a read-only database snapshot, **not a live
process/health indicator**. Removed watch arguments leave historical repository
snapshots visible with their old timestamps. GitHub pagination is not atomic;
future dispatch must refetch the exact revision and resolve trusted policy.
Observation IDs do not include policy and must not be reused as review node IDs.

Schema version 2 adds a private proposal-session table, migrating version 1
transactionally while retaining intake records. The store uses the CGO-free `modernc.org/sqlite` driver. The state directory
must be mode 0700, database mode 0600. WAL files are private within that directory.
Never delete `owner.lock` while an owner runs; the kernel releases its lock on exit.
Back up the database using a consistent SQLite backup or with the service stopped,
including any remaining WAL; copying a live database file alone is insufficient.

## Scoped automatic review

The separate [`service watch` command](evaluation.md) opts into one PR, trusted
review/judge policies and a human allowlist. It publishes starting, verdict and
scorecard comments, and polls intent replies. It reuses this store and ownership
lock; run one owner per installation. Supplied boot templates still select intake
only. Watch requires a matching repository checkout and permits trusted checks
to execute PR code as the OS user; it is not an execution sandbox.

## Boot installation

Templates are provided in `deploy/`; installation remains an explicit operator
step. Inspect repository arguments and paths first. Neither building nor copying
a template registers an App or starts inference.

For **Linux/systemd**, create the dedicated `ferretta` system account/group and
install the binary at `/usr/local/bin/ferretta`. Provision
`/etc/ferretta/github.json` and a mode-0600 key readable by that account, with an
absolute key path in the JSON. Install `deploy/ferretta.service` at
`/etc/systemd/system/ferretta.service`, then run `systemctl daemon-reload` and
`systemctl enable --now ferretta`. Systemd creates the private `/var/lib/ferretta`
state directory. Read logs using `journalctl -u ferretta` and inspect state as the
service account. The supplied sandbox is appropriate for intake only; it is not
an execution sandbox for future model tools.

For **macOS/launchd**, provision a dedicated `_ferretta` service account using
the machine's account-management policy. Install the binary at
`/usr/local/libexec/ferretta` (with a traversable, root-owned `libexec` directory);
create service-owned mode-0700 `/var/db/ferretta` and
`/usr/local/etc/ferretta`. Put the App config/key in the latter (key mode 0600),
and provision `/var/log/ferretta.log` as service-owned mode 0600. Install
`deploy/com.ferretta.daemon.plist` as root-owned mode 0644 under
`/Library/LaunchDaemons/`, then run `launchctl bootstrap system
/Library/LaunchDaemons/com.ferretta.daemon.plist` with administrator privileges.
Inspect using `launchctl print system/com.ferretta.daemon`. This is a LaunchDaemon
that starts at boot, independent of desktop login. Validate any edited plist
with `plutil -lint` before installation.

Keep credentials available to the service without desktop keychain unlock.
The current bootstrap is a protected key file; OS credential brokers are future
adapters. Restart after replacing App configuration/key material because the
connection is loaded once per process. No personal GitHub login fallback exists.

Reference semantics: [launchd startup and daemons](https://developer.apple.com/library/archive/documentation/MacOSX/Conceptual/BPSystemStartup/Chapters/CreatingLaunchdJobs.html),
[systemd service execution](https://github.com/systemd/systemd/blob/main/man/systemd.exec.xml).

## Installation validation (2026-09-29)

The CGO-disabled binary from commit `8a0c60f` was exercised on real hosts:

| Host | Environment | Verified | Remaining |
| --- | --- | --- | --- |
| jepsen | Ubuntu 26.04.1, amd64 | System installation, non-login service account, unit validation, enable/disable, start/stop/restart, clean SIGTERM exit, systemd recovery after SIGKILL, SQLite persistence, private file permissions, concurrent status reads and rejection of a second owner | GitHub App provisioning/live polling; actual reboot test |
| alpaca | macOS 26.6.2, arm64 | Plist validation and temporary user-domain launchd startup, forced restart, retained SQLite data and rejection of a second owner | Administrator-run installation and system-domain LaunchDaemon test; GitHub App provisioning/live polling; actual reboot test |

The Ubuntu installation is retained **stopped and disabled**. Its binary is at
`/usr/local/bin/ferretta`, unit at `/etc/systemd/system/ferretta.service`, state
at `/var/lib/ferretta`, and empty configuration directory at `/etc/ferretta`.
After provisioning and verifying the App as the service account, enable it with
`sudo systemctl enable --now ferretta.service`.

The temporary Mac job was unloaded and removed. A user-domain test does not
establish operation at boot or under the dedicated service account. Mac preflight
found `/usr/local/bin` was root-only on alpaca, so the LaunchDaemon template now
uses `/usr/local/libexec/ferretta`. Create that directory with traversal permission
for the service account; do not relax permissions on an unrelated existing
directory just to make the service run.

No machine was rebooted. No GitHub credentials were installed, API requests made,
model responses generated, or PR code executed during these installation tests.
Missing App configuration was the expected polling failure. Service health and
successful authenticated PR intake must be validated separately.

## Remaining orchestration work

The scoped watcher implements a serial durable workflow and reconciled milestone
publication. General DAG scheduling, parallel waves, monetary reservations,
resource-grant parsing and automated repair/merge remain future work. The manual
`review` command remains separate and does not consume the intake queue.
