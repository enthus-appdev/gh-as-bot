# gh-as-bot

A [gh](https://cli.github.com/) extension that runs `gh` authenticated as a GitHub App installation, so reviews and comments are attributed to a `<app>[bot]` identity instead of your personal account.

Built for the case where Claude Code (or any local automation with full project context) needs to leave reviews on a PR that are visually and programmatically distinguishable from human reviews — without disturbing your day-to-day `gh auth` state.

## Install

```bash
gh extension install enthus-appdev/gh-as-bot
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

# Help
gh as-bot help
```

`gh as-bot <args>` mints a fresh installation access token, sets `GH_TOKEN` for the duration of the call, and `exec`s `gh` with your arguments. Your persistent `gh auth` (the "me" account) is never touched — open a new shell and you're still you.

## Configuration

`gh-as-bot` reads three environment variables:

| Variable | Description |
|----------|-------------|
| `GH_AS_BOT_APP_ID` | Numeric App ID from the GitHub App settings page |
| `GH_AS_BOT_INSTALLATION_ID` | Numeric installation ID for the org/account where the App is installed |
| `GH_AS_BOT_PRIVATE_KEY` | Either inline PEM contents (starting with `-----BEGIN`) or a path to a `.pem` file |

### Recommended: keychain or 1Password CLI

Don't keep the private key on disk in plaintext. Source it just-in-time:

```bash
# macOS keychain
export GH_AS_BOT_PRIVATE_KEY="$(security find-generic-password -s gh-as-bot -w)"

# 1Password CLI
export GH_AS_BOT_PRIVATE_KEY="$(op read 'op://Private/gh-as-bot/private-key')"
```

A shell function makes invocation seamless:

```bash
gh-bot() {
  GH_AS_BOT_APP_ID=123456 \
  GH_AS_BOT_INSTALLATION_ID=789012 \
  GH_AS_BOT_PRIVATE_KEY="$(op read 'op://Private/gh-as-bot/private-key')" \
  gh as-bot "$@"
}
```

## GitHub App setup

This extension authenticates as a GitHub App installation, so you need a GitHub App first.

1. **Create the App** — Org settings → Developer settings → GitHub Apps → New GitHub App.
2. **Permissions** (Repository):
   - `Pull requests: Read & write` — leave reviews and review comments
   - `Contents: Read` — needed by `gh` for most read paths
   - `Issues: Read & write` — comment on issues / PR conversation
3. **Webhook**: not needed for this use case — uncheck "Active" on the webhook section.
4. **Generate a private key** — App settings → "Private keys" → "Generate a private key". Download the `.pem` file. You won't be able to download it again.
5. **Install the App** on your org or specific repos. After installing, the URL will look like `.../installations/123456/...` — that's your installation ID.
6. **Find the App ID** in the App settings header.

The bot identity that posts reviews will be `<app-slug>[bot]` (e.g. `negsoft-claude[bot]`).

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
