# ban-bot

A lightweight Discord bot that automatically bans users who post messages in a designated honeypot channel, or spam the same message across 3+ channels, and purges their recent messages across the server.

## Features

- 🍯 Monitors a honeypot channel for non-bot messages
- 📢 Detects duplicate messages sent to 3+ channels within the cache TTL
- 🔨 Automatically bans offending users
- 🧹 Purges cached messages from banned users across all channels
- ⚡ Minimal resource usage (~100MB RAM)
- 🐧 Native systemd deployment support

## Requirements

- Go 1.23+
- A Discord bot token with the `GUILD_MESSAGES` privileged intent enabled

## Environment Variables

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `DISCORD_TOKEN` | Yes | - | Discord bot token |
| `HONEYPOT_CHANNEL_ID` | Yes | - | Channel ID to monitor |
| `CACHE_TTL_SECONDS` | No | 60 | How long to cache messages for purge |
| `CACHE_MAX_PER_USER` | No | 100 | Max cached messages per user |

## Quick Start

```bash
# Build the binary
make build

# Run tests
make test

# Run locally (requires env vars)
make run
```

## Deployment

The project includes an automated, idempotent install script that sets up a systemd service running under an unprivileged user.

### Prerequisites

- Linux with systemd
- `sudo` access
- `.envrc` file in the project root (direnv-style with `export VAR=value`)

### Install

```bash
make install
```

This will:

- Build the binary if source files changed
- Create an unprivileged `ban-bot` system user
- Install to `/opt/ban-bot`
- Create a systemd service that starts on boot
- Preserve existing `/opt/ban-bot/env` on updates

### View Logs

```bash
make logs
```

Or manually:

```bash
sudo journalctl -u ban-bot -f
```

### Service Management

```bash
sudo systemctl status ban-bot
sudo systemctl restart ban-bot
sudo systemctl stop ban-bot
```

## Makefile Targets

| Target | Description |
|--------|-------------|
| `build` | Compile the binary |
| `test` | Run Go tests |
| `run` | Build and run locally |
| `install` | Build and install systemd service |
| `logs` | Follow service logs |
| `clean` | Remove built binary |

## Why This Bot?

Some Discord servers use a hidden or restricted channel as a honeypot to catch spam bots and self-promoters who scrape invite links or crawl server channels. When a user posts in the honeypot, this bot immediately bans them and cleans up their other messages.

The bot also hashes message content to detect users sending the exact same message to 3 or more channels within the cache TTL window, and applies the same ban and purge to them.
