# Silo AniList Sync

An AniList watch-sync provider for [Silo Server](https://github.com/Silo-Server/silo-server).

## Current status

`main` contains the `v0.3.0` manifest and targets
[`silo-plugin-sdk` v0.13.0](https://github.com/Silo-Server/silo-plugin-sdk/releases/tag/v0.13.0).
Plugin-backed watch providers landed in Silo Server through
[Silo Server PR #475](https://github.com/Silo-Server/silo-server/pull/475).
Until a Silo release includes that host adapter, this plugin requires a Silo
build from current `main`.

The `v0.1.x` release line uses the legacy event-consumer integration. Use
`v0.3.x` or a build from `main` for the host-owned watch-provider integration
described below.

## Features

- Exports anime movies and episodes when playback passes the configured watched
  threshold.
- Optionally exports items manually marked watched through a separate,
  disabled-by-default setting.
- Imports mapped AniList watch history into Silo.
- Supports AniList OAuth authorization codes and manually issued access tokens.
- Preserves completed entries and never lowers AniList progress.
- Uses AniBridge, Anime-Lists, and ARM mapping sources without guessing by title.
- Supports Linux amd64, Linux arm64, and Apple silicon macOS.

## Setup

1. Add the repository URL below to Silo's plugin repositories.
2. Install **AniList Sync** from Silo's plugin catalog.
3. Create an AniList OAuth application under
   [AniList developer settings](https://anilist.co/settings/developer).
4. Enter the AniList client ID and client secret in the plugin settings.
5. Set **Playback completion threshold** to the same watched percentage used by
   Silo.
6. Connect AniList from the desired Silo profile using OAuth or a manually
   issued AniList access token.
7. Enable **Sync manually marked watched items** only if manual marks should
   advance AniList.

### Provider settings

| Setting | Default | Purpose |
| --- | --- | --- |
| Client ID | Required | Numeric AniList OAuth application ID. |
| Client secret | Required | AniList OAuth application secret; Silo stores it as a secret. |
| Sync manually marked watched items | Off | Also export items marked watched without completed playback. |
| Playback completion threshold | 90% | Percentage playback must exceed before AniList progress advances; valid range 1–99. |

## Install and update through Silo

### Add the shared repository

1. Sign in to Silo as an administrator.
2. Open **Administration → Plugins** and select the **Catalog** tab.
3. Under **Repositories**, select **Add**.
4. Enter `Crowquillx plugins` as the repository name.
5. Enter this URL:

   ```text
   https://raw.githubusercontent.com/crowquillx/crowquillx-silo-plugins/main/repository.json
   ```

6. Select **Add**. **AniList Sync** will appear in the catalog.
7. Select **Install** on the AniList Sync card.
8. Return to the **Installed** tab and select **Configure** to enter the AniList
   OAuth client ID, client secret, and playback completion threshold.
9. Connect AniList from each Silo profile that should synchronize watch state.

The shared catalog can add future crowquillx plugins without requiring another
repository URL in Silo. Existing installations may keep using the legacy
AniList-only URL:

```text
https://github.com/crowquillx/silo-anilist-sync/releases/latest/download/repository.json
```

Every `v*` tag builds all supported binaries, calculates their SHA-256
checksums, embeds the release manifest in the plugin's repository index, and
publishes all assets. The shared catalog refreshes from that versioned release
boundary. Silo selects the binary for its platform and verifies its checksum
before installation. Repository installations default to Silo's automatic
update policy; operators may instead select notification-only or manual
updates.

## Architecture

Silo owns:

- AniList OAuth application secrets and callback state
- encrypted credentials scoped to a Silo user and profile
- durable desired-state events and stable event IDs
- delivery ordering, retries, rate-limit deferral, and reconciliation
- rich movie/episode identity captured at watch completion

The plugin remains a stateless provider adapter. It:

1. Builds AniList authorization URLs and exchanges authorization codes.
2. Validates credentials with AniList's `Viewer` query.
3. Imports AniList watch history by expanding list progress into completed
   movies and episodes and reverse-mapping them through AniBridge.
4. Maps Silo TVDB/TMDB/IMDb movie and episode identity through the daily
   [AniBridge v3 mappings](https://github.com/anibridge/anibridge-mappings),
   falling back through [Anime-Lists](https://github.com/Anime-Lists/anime-lists)
   and the [ARM mapping service](https://github.com/BeeeQueue/arm-server) when
   AniBridge has no direct export mapping.
5. Advances AniList on completed playback stop events. Manual watched marks use
   the same convergent update only when their separate plugin toggle is enabled.
6. Reads the existing AniList list entry and applies absolute, monotonic
   progress through `SaveMediaListEntry`.
7. Returns typed applied, no-change, rejected, retry, rate-limit, and credential
   outcomes to Silo's durable worker.

Credentials and OAuth flow data are transient RPC inputs. The plugin does not
persist or log them.

## Mapping policy

- Resolve every available TVDB and TMDB descriptor and require convergence.
- Use IMDb only as a movie fallback; AniBridge has no populated IMDb-series
  mappings.
- Prefer direct AniBridge mappings. Use Anime-Lists only to resolve an external
  identity and episode to AniDB. Prefer AniBridge's AniDB episode projection
  when available, then use ARM as the independent AniDB-to-AniList fallback for
  regular episodes and movies.
- Support AniBridge v3 ratio semantics.
- Reject conflicting mappings, non-contiguous target ranges, and mapped
  progress beyond AniList's known episode count.
- Never guess by title.
- Import only identities that AniBridge can reverse-map; title-only matches and
  export-only Anime-Lists/ARM fallbacks are not safe for history import.
- Retain the last valid in-memory datasets if a daily refresh fails.

## AniList progress policy

- Never lower remote progress.
- Existing completed entries never regress.
- Planning entries become current after a watched event.
- Current entries become completed at the known final episode.
- Paused, dropped, and repeating states are preserved.
- Import each mapped episode up to AniList's absolute progress as watched.
- Sync completed playback after it passes the configured completion threshold,
  which defaults to Silo's default of 90%.
- Ignore start, pause, and incomplete stop events because AniList has no
  intra-episode scrobble state.
- Do not sync history reconciliation or manually marked watched items unless
  **Sync manually marked watched items** is enabled.
- Do not advertise resume-progress import, unwatch, favorites, or watchlists
  where AniList cannot preserve Silo's item-level semantics without destructive
  or lossy side effects.
- Duplicate desired-state events are convergent; they never increment repeat
  count.

## Development

Requires Go 1.26 and `silo-plugin-sdk` v0.13.0 or newer:

```fish
cd /path/to/silo-anilist-sync
CGO_ENABLED=0 go test ./...
CGO_ENABLED=0 go build -o plugin .
./plugin manifest | jq
```

On NixOS:

```fish
nix shell nixpkgs#go --command fish -c 'CGO_ENABLED=0 go test ./...'
```

### Releases

Set the target version in `manifest.json`, then push the matching `vX.Y.Z` tag.
The release workflow validates that the tag and manifest agree, cross-compiles
every supported platform, publishes checksums and binaries, and generates the
`repository.json` consumed by Silo. The regular CI workflow performs the same
cross-build and index-generation checks on pull requests and `main`.

## Privacy and upstream services

The plugin sends AniList media IDs and absolute progress to AniList and reads
the connected account's anime-list progress for watched imports. It retrieves
the public AniBridge and Anime-Lists mapping artifacts from GitHub; fallback
resolution sends AniDB IDs to the public ARM service. No AniList credentials or
Silo user information are sent to mapping services. Silo passes decrypted
credentials only over the local plugin gRPC channel for calls that need them.

## Attribution

Many thanks to [AniBridge](https://github.com/anibridge/anibridge-mappings),
[Anime-Lists](https://github.com/Anime-Lists/anime-lists), and
[ARM](https://github.com/BeeeQueue/arm-server) for making reliable
cross-provider anime mapping possible.

## License

MIT
