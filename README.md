# gh-as-bot

A [gh](https://cli.github.com/) extension that runs `gh` authenticated as a GitHub App installation, so reviews and comments are attributed to a `<app>[bot]` identity instead of your personal account.

Built for the case where Claude Code (or any local automation with full project context) needs to leave reviews on a PR that are visually and programmatically distinguishable from human reviews — without disturbing your day-to-day `gh auth` state.

## Quickstart

```bash
gh extension install enthus-appdev/gh-as-bot
gh as-bot setup
```

`gh as-bot setup` walks you through creating the GitHub App, points you at the right URLs with the required permissions called out, verifies your credentials by minting a real installation token, optionally stashes the private key in your macOS keychain, and prints the exact shell-profile snippet to paste into `~/.zshrc`.

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
| `GH_AS_BOT_APP_ID` | Numeric App ID from the GitHub App settings page |
| `GH_AS_BOT_INSTALLATION_ID` | Numeric installation ID for the org/account where the App is installed |
| `GH_AS_BOT_PRIVATE_KEY` | Either inline PEM contents (starting with `-----BEGIN`) or a path to a `.pem` file |
| `GITHUB_API_URL` | Optional; override API base for GHES |

Recommended sourcing patterns for the private key (don't keep `.pem` on disk in plaintext):

```bash
# macOS keychain (what `gh as-bot setup` writes for you)
# The key is stored base64-encoded, so decode it on read — macOS
# `security -w` hex-mangles any value containing newlines (a raw PEM has
# many), which would otherwise round-trip to garbage.
export GH_AS_BOT_PRIVATE_KEY="$(security find-generic-password -s gh-as-bot -w | base64 -D)"

# 1Password CLI
export GH_AS_BOT_PRIVATE_KEY="$(op read 'op://Private/gh-as-bot/private-key')"
```

If you stashed the key into the keychain yourself (rather than via `gh as-bot setup`), store it base64-encoded so the read above works: `base64 < your-app.pem | security add-generic-password -s gh-as-bot -a default -U -w "$(cat -)"`.

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
