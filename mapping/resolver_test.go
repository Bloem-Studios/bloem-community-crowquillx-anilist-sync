package mapping

import "testing"

func TestResolveEpisode(t *testing.T) {
	dataset := Dataset{
		"tvdb_show:100:s2": {
			"anilist:42": {"1-12": "1-12"},
		},
	}
	targets, err := dataset.Resolve("tvdb", "100", 2, 7)
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 1 || targets[0] != (Target{AniListID: 42, Episode: 7}) {
		t.Fatalf("targets = %#v", targets)
	}
}

func TestResolveIdentityMapping(t *testing.T) {
	dataset := Dataset{"tmdb_show:55:s1": {"anilist:9": {}}}
	targets, err := dataset.Resolve("tmdb", "55", 1, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 1 || targets[0].Episode != 3 {
		t.Fatalf("targets = %#v", targets)
	}
}

func TestResolveRatiosAndDiscontinuousTargets(t *testing.T) {
	cases := []struct {
		name    string
		ranges  map[string]string
		episode int
		want    int
	}{
		{"two targets per source", map[string]string{"1-12": "1-24|2"}, 3, 6},
		{"two sources per target", map[string]string{"1-12": "1-6|-2"}, 4, 2},
		{"discontinuous", map[string]string{"1-6": "1-3,5-7"}, 4, 5},
		{"open ended", map[string]string{"4-": "10-"}, 6, 12},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok, err := mapEpisode(tc.ranges, tc.episode)
			if err != nil {
				t.Fatal(err)
			}
			if !ok || got != tc.want {
				t.Fatalf("mapEpisode() = %d, %v; want %d, true", got, ok, tc.want)
			}
		})
	}
}

func TestMovieDescriptor(t *testing.T) {
	dataset := Dataset{"tmdb_movie:1": {"anilist:2": {}}}
	targets, err := dataset.Resolve("tmdb", "1", -1, 1)
	if err != nil || len(targets) != 1 || targets[0].AniListID != 2 {
		t.Fatalf("targets = %#v, err = %v", targets, err)
	}
}
