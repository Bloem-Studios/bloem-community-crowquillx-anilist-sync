# Silo AniList Sync

An AniList watch-sync provider for [Silo Server](https://github.com/Silo-Server/silo-server).

## Current status

> [!WARNING]
> Watch-history import is new and still stabilizing. Do not rely on it as the
> only copy of your watch state. The device activation flow shipped in v0.5.0
> and is live in production.

`main` contains the `v0.7.0` manifest and targets
[`silo-plugin-sdk` v0.13.0](https://github.com/Silo-Server/silo-plugin-sdk/releases/tag/v0.13.0).
Plugin-backed watch providers landed in Silo Server through
[Silo Server PR #475](https://github.com/Silo-Server/silo-server/pull/475).
Until a Silo release includes that host adapter, this plugin requires a Silo
build from current `main`.

The `v0.1.x` release line uses the legacy event-consumer integration. Use
`v0.5.x` or a build from `main` for the host-owned watch-provider integration
described below.

## Features

- Exports anime movies and episodes when playback passes the configured watched
  threshold.
- Optionally exports items manually marked watched through a separate,
  disabled-by-default setting.
- Optionally limits exports to anime already on the connected AniList account's
  list, preventing false matches from adding unexpected titles.
- Adds an optional Silo watched check for Neptune/Jellycompat players that
  report a stop at zero.
- Imports mapped AniList watch history into Silo.
- Connects profiles through a device activation flow. The browser encrypts
  the token to a key derived from the activation code, and the bridge never
  sees it.
- Preserves completed entries and never lowers AniList progress.
- Uses AniBridge, Anime-Lists, and ARM mapping sources without guessing by title.
- Supports Linux amd64, Linux arm64, and Apple silicon macOS.

## Setup

1. Add the repository URL below to Silo's plugin repositories.
2. Install **AniList Sync** from Silo's plugin catalog.
3. Set **Playback completion threshold** to the same watched percentage used by
   Silo.
4. In the desired Silo profile, open **Settings → Watch Providers**, find
   **AniList**, and select **Connect**. Silo shows a code and a button that
   opens the activation page at https://anilist.crowquill.dev. Recent Silo
   builds embed the code in the link, so you may not be asked to type it. If
   the page asks for the code, enter it. Approve access on AniList and return
   to Silo. The connection completes on its own and shows your account.

Enable **Sync manually marked watched items** only if manual marks should
advance AniList.

### Neptune and Jellycompat watched verification

Some players send a final stop with position zero after Silo has already saved
an episode as watched. The ordinary AniList provider skips that stop because
its completion percentage is zero. Version 0.7.0 adds **AniList with Silo
watched check** for this case.

To enable it for a profile:

1. Update AniList Sync to v0.7.0 or later.
2. Obtain an AniList access token using this authorization link:
   [Authorize AniList Sync](https://anilist.co/api/v2/oauth/authorize?client_id=49797&response_type=token).
   Approve access and copy the token shown by AniList.
3. Create a Silo API key for the account that owns the profile. Get the current
   profile's `id` from `GET /api/v1/profiles`, authenticated with that key.
4. In that same profile, open **Settings → Watch Providers → AniList with Silo
   watched check → Connect**. Paste the **AniList token** in the main API-key
   field. Enter the native Silo server URL, **Silo API key**, and **Silo profile
   ID** in the additional fields. Use HTTPS for remote servers. The URL must
   be reachable from the Silo server running the plugin.
5. Once the new connection succeeds, disconnect the ordinary **AniList**
   provider for this profile to avoid duplicate imports and exports. Enable
   playback scrobbling on the new connection and select your import settings.

The plugin checks that the API key can access the selected profile when you
connect and again before each watched lookup. Credentials and the selected
profile are stored with that connection, encrypted by Silo. This setup needs
no database access or Silo server changes. The separate provider is necessary
because Silo currently supports connection settings only for API-key auth.

Only a stop with zero position, zero completion percentage, a known duration,
and an exact Silo item ID triggers verification. The plugin reads
`GET /api/v1/catalog/items/{id}` with `X-Profile-Id` and proceeds only if the
returned item ID matches and `user_state.played` is true. Unwatched or missing
items stay unchanged. Temporary API failures return a retryable error.
Ordinary completed playback, mapping checks, the existing-entry restriction,
and monotonic AniList progress use the same rules as the standard provider.

Select the same profile as the watch-provider connection. Silo does not send
the connection's profile ID to the plugin, so it can validate ownership but
cannot detect a different accessible profile selected by mistake. Existing
watched state can come from an earlier watch, an import, or a manual mark;
this fallback confirms saved watched state, not how the latest session ended.

The new connection handles future stops. It does not automatically replay
previously acknowledged stops, including episodes missed before installation.

### Provider settings

| Setting | Default | Purpose |
| --- | --- | --- |
| Sync manually marked watched items | Off | Also export items marked watched without completed playback. |
| Playback completion threshold | 90% | Percentage playback must exceed before AniList progress advances; valid range 1–99. |
| Full import scan | Off | Force the next watched-import to rescan the complete AniList list instead of only entries changed since the previous scan. Turn it off again afterwards to resume fast incremental syncs. |
| Only update anime already on AniList | Off | Export progress only for titles already on the connected account's list. Add titles to Plan to Watch before watching. |

Watched imports are incremental: the plugin records a checkpoint when a list
snapshot is fetched and, on later syncs, only entries AniList reports as
updated at or after that moment are reverse-mapped and reported to Silo.
Unchanged lists cost a single AniList request and finish in seconds. Silo's
watched import only adds history, so incremental traversals are safe; the
full-scan toggle re-reads everything when you want a from-scratch pass.

### Reducing false matches

Enable **Only update anime already on AniList** in the plugin's configuration
to prevent exports from creating new AniList entries. Add the anime you want
to sync to **Plan to Watch** on AniList before watching in Silo. The restriction
applies to completed playback and enabled manual watched exports. Existing
installations keep automatic additions until this setting is enabled.

The setting applies to the whole plugin installation, but each export checks
the connected AniList account's own list. Profiles connected to different
AniList accounts can therefore allow different titles. Profiles sharing an
AniList account share its list. Skipped events are acknowledged without a
change; adding a title later does not automatically replay those events.

This reduces unwanted additions, but an incorrect match can still update a
title already on your list. It does not restrict watched-history imports.
Disable watched-history import for the profile in **Settings → Watch
Providers → AniList** if imported matches are affecting unrelated Silo items.

The current watch-provider interface does not pass Silo profile or library
identity to this plugin. Per-profile library selection needs host support;
this option works with the existing interface and requires no server changes.

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

6. Select **Add**. AniList Sync appears in the catalog.
7. Select **Install** on the AniList Sync card.
8. Return to the **Installed** tab and select **Configure** to set the
   playback completion threshold.
9. Follow the connect steps under [Setup](#setup) for each Silo profile that
   should synchronize watch state.

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

- encrypted credentials scoped to a Silo user and profile
- durable desired-state events and stable event IDs
- delivery ordering, retries, rate-limit deferral, and reconciliation
- rich movie/episode identity captured at watch completion

The plugin remains a stateless provider adapter. It:

1. Runs the device authorization flow, adapting AniList's implicit grant
   through the connect bridge; the bridge stores only encrypted envelopes and
   the plugin decrypts the token with a key derived from the user code.
2. Validates credentials with AniList's `Viewer` query.
3. Imports AniList watch history by expanding list progress into completed
   movies and episodes and reverse-mapping them through AniBridge; the account
   list is fetched in a single request and cached briefly.
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
8. Paces AniList requests under the published rate limit and adapts the spacing
   from AniList's `X-RateLimit` headers.

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
`repository.json` that Silo consumes. The regular CI workflow performs the same
cross-build and index-generation checks on pull requests and `main`.

## Privacy and upstream services

The plugin sends AniList media IDs and absolute progress to AniList and reads
the connected account's anime-list progress for watched imports. It retrieves
the public AniBridge and Anime-Lists mapping artifacts from GitHub; fallback
resolution sends AniDB IDs to the public ARM service. The plugin sends
no AniList credentials or Silo user information to mapping services. Silo
passes decrypted credentials only over the local plugin gRPC channel for
calls that need them.

The [connect bridge](https://github.com/crowquillx/anilist-connect-bridge)
receives only a SHA-256 hash of the user code and an AES-256-GCM ciphertext.
The browser encrypts the AniList token to a key derived from the code before
it is sent, so the bridge cannot read it; the bridge stores only the
encrypted envelope for the 15-minute connection window.

## Attribution

Many thanks to [AniBridge](https://github.com/anibridge/anibridge-mappings),
[Anime-Lists](https://github.com/Anime-Lists/anime-lists), and
[ARM](https://github.com/BeeeQueue/arm-server) for making reliable
cross-provider anime mapping possible.

## License

MIT
