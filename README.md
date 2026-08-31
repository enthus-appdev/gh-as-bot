# gh-as-bot

A [gh](https://cli.github.com/) extension that runs `gh` authenticated as a GitHub App installation, so reviews and comments are attributed to a `<app>[bot]` identity instead of your personal account.

Built for the case where Claude Code (or any local automation with full project context) needs to leave reviews on a PR that are visually and programmatically distinguishable from human reviews — without disturbing your day-to-day `gh auth` state.

## Quickstart

```bash
gh extension install enthus-appdev/gh-as-bot
gh as-bot setup
```

`gh as-bot setup` walks you through creating the GitHub App, points you at the right URLs with the required permissions called out, verifies your credentials by minting a real installation token, optionally stashes the private key in your macOS keychain, and — with your confirmation — writes the export line straight into your shell profile (it detects the rc but lets you correct the path, since `$SHELL` can lie). The lines go in a marked `# >>> gh-as-bot:<context> >>>` block, so re-running setup updates that block in place and multiple contexts coexist without clobbering each other.

After setup, verify with:

```bash
gh as-bot doctor
```

## Usage

```bash
# Run any gh command as the bot installation
gh as-bot pr review 123 -c -b "Bot review here."
gh as-bot pr comment 123 -b "Heads up: this needs a migration."
gh as-bot api repos/{owner}/{repo}/pulls/123/reviews -f event=COMMENT ...

# Run under a specific identity (see Contexts below)
gh as-bot --context org pr review 123 -c -b "Bot review here."

# Manage named identities
gh as-bot context list

# Print just the installation token (useful for piping)
gh as-bot --token

# Verify config and credentials
gh as-bot doctor

# Re-run guided setup
gh as-bot setup

# Help
gh as-bot help
```

`gh as-bot <args>` mints a fresh installation access token, sets `GH_TOKEN` for the duration of the call, and `exec`s `gh` with your arguments. Your persistent `gh auth` (the "me" account) is never touched — open a new shell and you're still you.

## Configuration reference

`gh as-bot setup` writes the env vars below for you. If you'd rather configure manually:

| Variable | Description |
|----------|-------------|
| `GH_AS_BOT_APP_ID` | Numeric App ID from the GitHub App settings page (legacy single-App mode) |
| `GH_AS_BOT_INSTALLATION_ID` | Numeric installation ID for the org/account where the App is installed (legacy single-App mode) |
| `GH_AS_BOT_PRIVATE_KEY` | Inline PEM, a `.pem` path, or a `keychain:<service>` reference (legacy single-App mode) |
| `GH_AS_BOT_CONTEXT` | Active context name when `--context` is not passed (see [Contexts](#contexts-multiple-identities)) |
| `GH_AS_BOT_PRIVATE_KEY_<NAME>` | Per-context private key (PEM, path, or `keychain:<service>` reference), where `<NAME>` is the upper-cased context name |
| `GH_AS_BOT_CONFIG` | Optional; config file path (default `~/.config/gh-as-bot/config.json`) |
| `GITHUB_API_URL` | Optional; override API base for GHES |

Recommended sourcing patterns for the private key (don't keep `.pem` on disk in plaintext):

```bash
# macOS keychain (what `gh as-bot setup` writes for you)
# The key is stored base64-encoded, so decode it on read — macOS
# `security -w` hex-mangles any value containing newlines (a raw PEM has
# many), which would otherwise round-trip to garbage. `/usr/bin/base64` is
# pinned because the BSD `-D` decode flag differs from GNU coreutils' `-d`;
# a Homebrew GNU base64 ahead in PATH would otherwise silently yield an empty key.
export GH_AS_BOT_PRIVATE_KEY="keychain:gh-as-bot"

# 1Password CLI
export GH_AS_BOT_PRIVATE_KEY="$(op read 'op://Private/gh-as-bot/private-key')"
```

If you stashed the key into the keychain yourself (rather than via `gh as-bot setup`), store it base64-encoded so the read above works. `tr -d '\n'` guards against base64 implementations that wrap long lines (wrapped output would reintroduce the newline that breaks `security -w`):

```bash
security add-generic-password -s gh-as-bot -a default -U -w "$(base64 < your-app.pem | tr -d '\n')"
```

## Contexts (multiple identities)

If you need more than one bot identity — e.g. an org-owned App for org repos and a personal-owned App for personal repos — use **contexts**. Each context is a named credential set; you pick one explicitly per shell or per call.

GitHub's install model nudges you toward two **private** Apps rather than one public one: a private App installs only on its owner account, so org + personal means an org-owned App and a personal-owned App, each with its own key and installation. Neither needs to be made public.

### Why selection is always explicit

There is **no persisted "current context"**. `gh as-bot` exists precisely to avoid the global, cross-shell state of `gh auth switch` (see [below](#why-not-just-gh-auth-switch)). A mutable default in a shared config file would reintroduce exactly that: switching in one terminal would silently change the identity in every other concurrent session. So with contexts defined, you must select one via `--context <name>` (per call) or `GH_AS_BOT_CONTEXT` (per shell) — gh-as-bot never guesses an identity. With no contexts defined, it falls back to the legacy single-App env vars.

### Setup

Run `gh as-bot setup` once per App and give each a context name (e.g. `org`, `personal`). Setup writes the non-secret `app_id` + `installation_id` to `~/.config/gh-as-bot/config.json`, stashes that App's key in the keychain under service `gh-as-bot-<name>` (base64), and prints the rc lines to source.

You can also define a context non-interactively:

```bash
gh as-bot context add org --app-id 3995028 --installation-id 138796188
# stash that App's key under its own keychain service:
security add-generic-password -s gh-as-bot-org -a default -U -w "$(base64 < org-app.pem | tr -d '\n')"
# print the rc lines (key env var + selector) to add to your profile:
gh as-bot context export org
```

`gh as-bot context export <name>` emits:

```bash
export GH_AS_BOT_PRIVATE_KEY_ORG="keychain:gh-as-bot-org"
export GH_AS_BOT_CONTEXT="org"
```

Only a non-secret **key reference** lives in env; gh-as-bot resolves it from
Keychain when a bot command needs a token. `app_id` and `installation_id` live in
the non-secret config file. **Never put a private key (PEM) in `config.json`** —
it holds App/installation IDs only; keys belong in the keychain or your secrets
manager.

### Using contexts

```bash
gh as-bot context list                     # show defined contexts
gh as-bot context current                  # show the active context
gh as-bot --context org pr review 5 -c -b "Bot review"   # one-off override
GH_AS_BOT_CONTEXT=personal gh as-bot pr comment 1 -b "…" # per shell
gh as-bot context remove old-ctx
```

For concurrent sessions, set `GH_AS_BOT_CONTEXT` per shell (or pass `--context`); each window keeps its own identity with no cross-talk.

> A companion Claude Code guard hook (shipped separately, like the sqlcmd context guard) can block `gh as-bot` calls that don't carry an explicit context, so an accidental wrong-identity post is caught before it runs. It's a cooperative nudge, not a security boundary.

## One App per person (recommended)

`gh as-bot` is designed for the **per-person** pattern: each developer creates their own GitHub App under their personal account, with a name like `<your-username>-claude`. PR comments are then attributed to e.g. `hinne-claude[bot]` vs `marcus-claude[bot]`, so it's always clear whose Claude Code session produced a review.

Why per-person:

- **Attribution.** Reviews link back to the person whose context produced them — important when bot output is shaped by individual project memory and prompts.
- **Blast radius.** Revoking one developer's App or rotating their key affects only them.
- **Self-service.** No org admin coordination needed to onboard a new dev.
- **No shared private key.** Each dev controls and stashes their own `.pem`.

Trade-off: N Apps to manage instead of one shared bot. For a small team this is barely visible; for a large team you may prefer a shared org-owned App, in which case use `https://github.com/organizations/<your-org>/settings/apps/new` instead — `gh as-bot` itself works identically either way.

## GitHub App reference

`gh as-bot setup` covers this interactively. For reference, the App needs:

- **Owner**: your personal account (per-person) or your org (shared bot).
- **Name**: must be globally unique across GitHub (e.g. `<your-username>-claude`).
- **Homepage URL**: required — any valid URL incl. scheme (e.g. `https://example.com`). Cosmetic for our use; it just has to parse.
- **"Request user authorization (OAuth) during installation"**: uncheck it. Leaving it on forces a Callback URL we don't use — gh-as-bot authenticates as an App installation, not via the OAuth user flow.
- **"Where can this GitHub App be installed?"**: Any account — so personal Apps can still install on org repos.
- **Repository permissions**:
  - `Pull requests: Read & write` — post reviews and review comments
  - `Contents: Read` — `gh` needs this for most read paths
  - `Issues: Read & write` — post issue / PR conversation comments
- **Webhook**: not used — uncheck "Active".
- **Private key**: generate from App settings → "Private keys". The `.pem` is shown once; setup helps you stash it.
- **Install** the App on the repos that should accept bot reviews.

The bot identity that posts reviews will be `<app-slug>[bot]` (e.g. `hinne-claude[bot]`).

## Why not just `gh auth switch`?

Two reasons:

1. `gh auth switch` is **global state** — switching changes the active account for every shell on your machine, including any other Claude Code session running concurrently.
2. GitHub App installations aren't users — `gh auth login` can't store an installation token, and you wouldn't want it to (1-hour TTL).

`gh as-bot` uses a **per-invocation `GH_TOKEN` override**: the bot identity is opt-in per command, your persistent `gh auth` is untouched, and tokens are minted fresh each call.

## Branch protection and human override

Configure your repo to keep human and bot reviews properly separated:

- **CODEOWNERS** lists humans only. Bot approvals don't satisfy required-reviewer rules.
- **Org setting** "Allow GitHub Actions to approve pull requests" — turn off, so the bot can comment / request changes but can't approve.
- If the bot leaves `REQUEST_CHANGES` and you've reviewed and agreed with it, dismiss the review (Settings → branch protection → "Allow specified actors to dismiss reviews") and approve as yourself. Your human approval is the override.

## Development

```bash
go build ./...
go test ./...
```

Zero external dependencies — JWT signing and the installation-token exchange use the standard library only.
