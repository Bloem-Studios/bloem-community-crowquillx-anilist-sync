package main

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	pluginv1 "github.com/Silo-Server/silo-plugin-sdk/pkg/pluginproto/silo/plugin/v1"
	publicmanifest "github.com/Silo-Server/silo-plugin-sdk/pkg/pluginsdk/manifest"
	sdkruntime "github.com/Silo-Server/silo-plugin-sdk/pkg/pluginsdk/runtime"
	"github.com/Silo-Server/silo-plugin-sdk/pkg/pluginsdk/runtimedefault"
	"github.com/crowquillx/silo-anilist-sync/anilist"
	"github.com/crowquillx/silo-anilist-sync/mapping"
	"github.com/crowquillx/silo-anilist-sync/silo"
)

var version string

//go:embed manifest.json
var manifestJSON []byte

type config struct {
	AniListToken string
	ProfileID    string
	SiloAPIKey   string
	SiloBaseURL  string
}

type syncJob struct {
	config    config
	profileID string
	contentID string
}

type server struct {
	runtimedefault.Server
	pluginv1.UnimplementedEventConsumerServer

	manifest *pluginv1.PluginManifest
	mappings *mapping.Client
	jobs     chan syncJob
	mu       sync.RWMutex
	config   config
}

func (s *server) GetManifest(context.Context, *pluginv1.GetManifestRequest) (*pluginv1.GetManifestResponse, error) {
	return &pluginv1.GetManifestResponse{Manifest: s.manifest}, nil
}

func (s *server) Configure(_ context.Context, req *pluginv1.ConfigureRequest) (*pluginv1.ConfigureResponse, error) {
	cfg := config{}
	for _, entry := range req.GetConfig() {
		values := entry.GetValue().AsMap()
		switch entry.GetKey() {
		case "account":
			cfg.AniListToken = stringValue(values["access_token"])
			cfg.ProfileID = stringValue(values["profile_id"])
		case "silo":
			cfg.SiloAPIKey = stringValue(values["api_key"])
			cfg.SiloBaseURL = strings.TrimRight(stringValue(values["base_url"]), "/")
		}
	}
	s.mu.Lock()
	s.config = cfg
	s.mu.Unlock()
	return &pluginv1.ConfigureResponse{}, nil
}

func (s *server) HandleEvent(ctx context.Context, req *pluginv1.HandleEventRequest) (*pluginv1.HandleEventResponse, error) {
	if req.GetEventName() != "user_state.changed" || req.GetPayload() == nil {
		return &pluginv1.HandleEventResponse{}, nil
	}
	payload := req.GetPayload().AsMap()
	played, hasPlayed := payload["played"].(bool)
	if stringValue(payload["change"]) != "watched" || !hasPlayed || !played {
		return &pluginv1.HandleEventResponse{}, nil
	}
	profileID := stringValue(payload["profile_id"])
	contentID := stringValue(payload["content_id"])
	if profileID == "" || contentID == "" {
		return &pluginv1.HandleEventResponse{}, nil
	}

	s.mu.RLock()
	cfg := s.config
	s.mu.RUnlock()
	if cfg.ProfileID != "" && cfg.ProfileID != profileID {
		return &pluginv1.HandleEventResponse{}, nil
	}
	if cfg.AniListToken == "" || cfg.SiloAPIKey == "" {
		return nil, fmt.Errorf("AniList and Silo credentials must be configured")
	}
	job := syncJob{config: cfg, profileID: profileID, contentID: contentID}
	select {
	case s.jobs <- job:
		return &pluginv1.HandleEventResponse{}, nil
	default:
		return nil, fmt.Errorf("AniList sync queue is full")
	}
}

func (s *server) runWorker() {
	for job := range s.jobs {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		err := s.processJob(ctx, job)
		cancel()
		if err != nil {
			slog.Error("AniList sync failed", "content_id", job.contentID, "profile_id", job.profileID, "error", err)
		}
	}
}

func (s *server) processJob(ctx context.Context, job syncJob) error {
	cfg := job.config
	if cfg.SiloBaseURL == "" {
		host := sdkruntime.Host()
		if host == nil {
			return fmt.Errorf("Silo host connection is not ready")
		}
		info, err := host.GetHostInfo(ctx)
		if err != nil {
			return fmt.Errorf("discover Silo URL: %w", err)
		}
		cfg.SiloBaseURL = firstNonEmpty(info.InternalBaseURL, info.PublicBaseURL)
	}
	siloClient := silo.NewClient(cfg.SiloBaseURL, cfg.SiloAPIKey, nil)
	dataset, err := s.mappings.Dataset(ctx)
	if err != nil {
		return err
	}
	targets, err := resolveItem(ctx, siloClient, dataset, job.profileID, job.contentID)
	if err != nil {
		return err
	}
	anilistClient := anilist.NewClient(cfg.AniListToken, nil)
	ids := make([]int, 0, len(targets))
	for id := range targets {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	for _, id := range ids {
		if err := anilistClient.AdvanceProgress(ctx, id, targets[id]); err != nil {
			return fmt.Errorf("sync AniList media %d: %w", id, err)
		}
	}
	return nil
}

func resolveItem(ctx context.Context, client *silo.Client, dataset mapping.Dataset, profileID, contentID string) (map[int]int, error) {
	item, err := client.GetItem(ctx, profileID, contentID)
	if err != nil {
		return nil, err
	}
	out := map[int]int{}
	switch item.Type {
	case "movie":
		err = addMappings(out, dataset, item, -1, 1)
	case "episode":
		series, resolveErr := client.GetItem(ctx, profileID, item.SeriesID)
		if resolveErr != nil {
			return nil, fmt.Errorf("resolve parent series: %w", resolveErr)
		}
		if item.SeasonNumber != nil && item.EpisodeNumber != nil {
			err = addMappings(out, dataset, series, *item.SeasonNumber, *item.EpisodeNumber)
		}
	case "season":
		series, resolveErr := client.GetItem(ctx, profileID, item.SeriesID)
		if resolveErr != nil {
			return nil, fmt.Errorf("resolve parent series: %w", resolveErr)
		}
		if item.SeasonNumber != nil && item.EpisodeCount != nil {
			err = addMappings(out, dataset, series, *item.SeasonNumber, *item.EpisodeCount)
		}
	case "series":
		episodes, resolveErr := client.GetEpisodes(ctx, profileID, item.ContentID)
		if resolveErr != nil {
			return nil, fmt.Errorf("list series episodes: %w", resolveErr)
		}
		for _, episode := range episodes {
			if err = addMappings(out, dataset, item, episode.SeasonNumber, episode.EpisodeNumber); err != nil {
				break
			}
		}
	}
	if err != nil {
		return nil, err
	}
	return out, nil
}

func addMappings(out map[int]int, dataset mapping.Dataset, item silo.Item, season, episode int) error {
	providers := [][2]string{{"tvdb", item.TvdbID}, {"tmdb", item.TmdbID}}
	var resolved map[int]int
	for _, provider := range providers {
		if provider[1] == "" {
			continue
		}
		targets, err := dataset.Resolve(provider[0], provider[1], season, episode)
		if err != nil {
			return err
		}
		if len(targets) == 0 {
			continue
		}
		candidate := make(map[int]int, len(targets))
		for _, target := range targets {
			candidate[target.AniListID] = target.Episode
		}
		if resolved != nil && !sameTargets(resolved, candidate) {
			return fmt.Errorf("AniBridge mappings disagree between Silo provider IDs for season %d episode %d", season, episode)
		}
		resolved = candidate
	}
	if resolved == nil && season < 0 && item.ImdbID != "" {
		targets, err := dataset.Resolve("imdb", item.ImdbID, season, episode)
		if err != nil {
			return err
		}
		resolved = make(map[int]int, len(targets))
		for _, target := range targets {
			resolved[target.AniListID] = target.Episode
		}
	}
	for id, progress := range resolved {
		if progress > out[id] {
			out[id] = progress
		}
	}
	return nil
}

func sameTargets(a, b map[int]int) bool {
	if len(a) != len(b) {
		return false
	}
	for id, progress := range a {
		if b[id] != progress {
			return false
		}
	}
	return true
}

func main() {
	manifest, err := loadManifest()
	if err != nil {
		panic(err)
	}
	srv := &server{manifest: manifest, mappings: mapping.NewClient(nil), jobs: make(chan syncJob, 256)}
	go srv.runWorker()
	sdkruntime.Serve(sdkruntime.ServeConfig{Servers: sdkruntime.CapabilityServers{
		Runtime:       srv,
		EventConsumer: srv,
	}})
}

func loadManifest() (*pluginv1.PluginManifest, error) {
	manifest, err := publicmanifest.Load(manifestJSON)
	if err != nil {
		return nil, fmt.Errorf("load embedded manifest: %w", err)
	}
	if version != "" {
		manifest.Version = version
	}
	path, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("resolve executable: %w", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read executable: %w", err)
	}
	checksum := sha256.Sum256(data)
	manifest.Checksum = hex.EncodeToString(checksum[:])
	return manifest, nil
}

func stringValue(value any) string {
	text, _ := value.(string)
	return strings.TrimSpace(text)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimRight(strings.TrimSpace(value), "/"); value != "" {
			return value
		}
	}
	return ""
}
