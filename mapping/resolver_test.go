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

func TestResolveRatios(t *testing.T) {
	cases := []struct {
		name    string
		ranges  map[string]string
		episode int
		want    int
		ok      bool
	}{
		{"two sources per target waits for boundary", map[string]string{"1-4": "20-21|2"}, 1, 0, false},
		{"two sources per target", map[string]string{"1-4": "20-21|2"}, 2, 20, true},
		{"three targets per source", map[string]string{"1-2": "20-25|-3"}, 1, 22, true},
		{"open ended", map[string]string{"4-": "10-"}, 6, 12, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok, err := mapEpisode(tc.ranges, tc.episode)
			if err != nil {
				t.Fatal(err)
			}
			if ok != tc.ok || got != tc.want {
				t.Fatalf("mapEpisode() = %d, %v; want %d, %v", got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestRejectsNonContiguousTarget(t *testing.T) {
	if _, _, err := mapEpisode(map[string]string{"1-6": "1-3,5-7"}, 4); err == nil {
		t.Fatal("expected non-contiguous target to be rejected")
	}
}

func TestMovieDescriptor(t *testing.T) {
	for _, provider := range []string{"tmdb", "tvdb"} {
		dataset := Dataset{provider + "_movie:1": {"anilist:2": {}}}
		targets, err := dataset.Resolve(provider, "1", -1, 1)
		if err != nil || len(targets) != 1 || targets[0].AniListID != 2 {
			t.Fatalf("%s targets = %#v, err = %v", provider, targets, err)
		}
	}
}
