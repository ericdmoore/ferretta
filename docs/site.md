# Site development and launch

The Hugo site lives in `site/` and uses `https://ferretta.cc/` as its canonical URL.
Its guides render the repository's existing Markdown through Hugo asset mounts,
so fixes to those documents also update the site. The root `install.sh` is mounted
at `/install.sh`; there is one source for the tested and published script.

## Local development

```sh
make tools-site       # downloads/builds the version in .hugo-version, into bin/tools
make site-check       # builds into bin/site and checks local links + installer identity
make site-serve       # http://localhost:1313
make test-installer   # fake downloads and commands; no network or real installation
```

Hugo 0.160.0 is pinned because it builds with Ferretta's pinned Go 1.26.5 toolchain.
The theme uses plain templates/CSS/JavaScript, without Node, Sass, a theme package,
remote fonts, or analytics. Updating Hugo is an explicit `.hugo-version` change;
local and CI builds use the same Make entry points. `make check` runs the Go suite,
including installer fixtures; the separate Pages workflow runs `make site-check`.
Only `bin/site` is uploaded. Review checkpoints and credentials are never included.

## Publish the site

The `Hugo site` workflow builds/checks pull requests without deploying them. It
deploys only `main`, on push or a manual workflow run, through the `github-pages`
environment. It does not create a release or publish draft release assets.

At inspection on 2026-09-29, the repository already had `ferretta.cc` configured
as its Pages custom domain, but used the legacy branch-root builder. Before the
first deployment after merging this branch:

1. In repository **Settings → Pages → Build and deployment**, select **GitHub Actions**.
2. Keep **Custom domain** set to `ferretta.cc`. The generated `CNAME` documents
   the choice, but an Actions deployment still needs the repository domain setting.
3. At the DNS provider, configure the apex (`@`) using GitHub's documented records:

   | Type | Name | Value |
   | --- | --- | --- |
   | A | @ | 185.199.108.153 |
   | A | @ | 185.199.109.153 |
   | A | @ | 185.199.110.153 |
   | A | @ | 185.199.111.153 |
   | CNAME | www | ericdmoore.github.io |

   Use these instead of conflicting apex parking records. Do not use
   `ericdmoore.github.io/ferretta` as a DNS target; DNS has no URL paths.
4. Verify domain ownership in your GitHub account's Pages settings using its
   supplied TXT record. Leave that record in place. Once DNS/certificate setup
   completes, enable **Enforce HTTPS** in repository Pages settings.
5. Deploy and verify the homepage, a docs page, and `https://ferretta.cc/install.sh`.
   The installer URL must return the script over HTTPS, not a Pages error page.

GitHub's [custom-domain documentation](https://docs.github.com/en/pages/configuring-a-custom-domain-for-your-github-pages-site/managing-a-custom-domain-for-your-github-pages-site)
and [domain verification instructions](https://docs.github.com/en/pages/configuring-a-custom-domain-for-your-github-pages-site/verifying-your-custom-domain-for-github-pages)
are authoritative for the current records and settings. DNS/account changes are
operator steps; this branch does not perform them.

## Publish an installable release

The version-tag workflow currently stages **draft** archives and checksums. At
inspection, only draft prerelease `v0.1.0-alpha.1` existed. The default installer
requires a published non-prerelease selected by GitHub's `/releases/latest`.
To make installation work, validate the intended revision and publish a release
with all four archives plus its matching checksum file. Release approval and
networked-test cadence remain separate decisions.

A published prerelease can be installed with `FERRETTA_VERSION` explicitly set.
Do not publish the old draft just to satisfy the installer if it lacks the
onboarding commands described by the site; cut and validate a suitable revision.
Remove the site's launch-prerequisite note only after public installation succeeds.

Installer fixtures cover platform mapping, Rosetta, checksums, pinned tags,
custom paths with spaces, upgrades, failures preserving an existing binary, and
explicit component selection. The fixture suite passed on macOS arm64 (alpaca) and Ubuntu amd64 (jepsen).
It never downloads models, starts services or writes to real installation directories. Test the first actual release on macOS and
Ubuntu before recommending the public install command broadly.
