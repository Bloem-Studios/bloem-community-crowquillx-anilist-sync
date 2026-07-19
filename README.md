# Silo AniList Sync

A [Silo Server](https://github.com/Silo-Server/silo-server) plugin that advances AniList anime progress when an item is marked watched.

## How it works

1. Subscribes to Silo's `user_state.changed` plugin event.
2. Handles watched changes with `played: true`.
3. Resolves the watched movie, series, season, or episode through Silo's catalog API.
4. Resolves all available TVDB/TMDB mappings with the daily [AniBridge mappings](https://github.com/anibridge/anibridge-mappings) dataset and rejects disagreements instead of guessing. IMDb is used only as a movie fallback because AniBridge has no populated IMDb-series mappings.
5. Reads the existing AniList list entry and only advances progress. It never lowers progress, preserves paused/dropped/repeating states, and marks current entries complete when AniList's episode count is reached.

AniBridge mappings are downloaded lazily and cached in memory for 24 hours. The dataset is not bundled into plugin releases.

## Requirements

- Silo Server with `event_consumer.v1` plugin support (SDK v0.10.0 or newer)
- An AniList OAuth access token
- A Silo API key belonging to the account whose profile is synced

## Setup

1. Install a binary from [Releases](https://github.com/crowquillx/silo-anilist-sync/releases), or upload a locally built binary to Silo.
2. In Silo's plugin settings, configure:
   - **AniList access token** — an OAuth bearer token with permission to update your list.
   - **Silo API key** — used only to read catalog metadata for the event's content ID.
   - **Silo profile ID** (recommended) — limits this installation to one profile.
   - **Silo base URL** (optional) — only needed when Silo's discovered internal/public URL is unreachable from the plugin process.
3. Mark an anime episode, season, series, or movie watched.

The Silo plugin contract currently supplies event-consumer configuration globally rather than per profile. This release therefore supports one AniList account per Silo server; set the profile ID to prevent other profiles from feeding that account.

## Supported behavior

- Episode watched: advances the mapped AniList entry to that episode.
- Season watched: advances the mapped entry to the season's episode count.
- Series watched: maps all cataloged episodes, including split-cour/separate AniList entries.
- Anime movie watched: sets progress to one.
- Unwatch events: ignored. AniList progress is never automatically reduced.
- Missing mappings: ignored without guessing by title.
- Conflicting provider mappings, non-contiguous target ranges, and progress beyond AniList's known episode count: rejected conservatively.

Queued work is serialized in memory to stay within Silo's event-handler deadline. Silo's event delivery currently has no durable event ID or replay mechanism, so work cannot yet survive a plugin/server restart.

## Development

Requires Go 1.26.

```fish
CGO_ENABLED=0 go test ./...
CGO_ENABLED=0 go build -o plugin .
./plugin manifest | jq
```

On NixOS without Go installed globally:

```fish
nix shell nixpkgs#go --command fish -c 'CGO_ENABLED=0 go test ./...'
```

## Privacy and upstream services

The plugin sends AniList media IDs and progress to AniList. It downloads the public AniBridge mapping JSON from GitHub Releases. Silo and AniList credentials remain in Silo's plugin configuration and are sent only to their respective services.

## Attribution

Cross-provider and episode mappings are supplied by [anibridge/anibridge-mappings](https://github.com/anibridge/anibridge-mappings), distributed under the MIT License and assembled from several upstream data sources documented by that project.

## License

MIT
