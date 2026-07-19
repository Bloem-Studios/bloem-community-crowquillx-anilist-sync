package main

import (
	"testing"

	"github.com/crowquillx/silo-anilist-sync/mapping"
	"github.com/crowquillx/silo-anilist-sync/silo"
)

func TestAddMappingsRequiresProviderConvergence(t *testing.T) {
	dataset := mapping.Dataset{
		"tvdb_show:1:s1": {"anilist:10": {"1-12": "1-12"}},
		"tmdb_show:2:s1": {"anilist:11": {"1-12": "1-12"}},
	}
	err := addMappings(map[int]int{}, dataset, silo.Item{TvdbID: "1", TmdbID: "2"}, 1, 3)
	if err == nil {
		t.Fatal("expected disagreeing provider mappings to fail")
	}
}

func TestAddMappingsAcceptsProviderConvergence(t *testing.T) {
	dataset := mapping.Dataset{
		"tvdb_show:1:s1": {"anilist:10": {"1-12": "1-12"}},
		"tmdb_show:2:s1": {"anilist:10": {"1-12": "1-12"}},
	}
	out := map[int]int{}
	if err := addMappings(out, dataset, silo.Item{TvdbID: "1", TmdbID: "2"}, 1, 3); err != nil {
		t.Fatal(err)
	}
	if out[10] != 3 {
		t.Fatalf("progress = %d, want 3", out[10])
	}
}
