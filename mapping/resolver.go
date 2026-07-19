package mapping

import (
	"fmt"
	"strconv"
	"strings"
)

type Target struct {
	AniListID int
	Episode   int
}

type Dataset map[string]map[string]map[string]string

func (d Dataset) Resolve(provider, id string, season, episode int) ([]Target, error) {
	descriptor := sourceDescriptor(provider, id, season)
	if descriptor == "" {
		return nil, nil
	}
	targets := d[descriptor]
	out := make([]Target, 0, len(targets))
	for targetDescriptor, ranges := range targets {
		if !strings.HasPrefix(targetDescriptor, "anilist:") {
			continue
		}
		anilistID, err := strconv.Atoi(strings.TrimPrefix(targetDescriptor, "anilist:"))
		if err != nil {
			continue
		}
		mapped, ok, err := mapEpisode(ranges, episode)
		if err != nil {
			return nil, fmt.Errorf("map %s to %s: %w", descriptor, targetDescriptor, err)
		}
		if ok {
			out = append(out, Target{AniListID: anilistID, Episode: mapped})
		}
	}
	return out, nil
}

func sourceDescriptor(provider, id string, season int) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}
	switch strings.ToLower(provider) {
	case "tvdb":
		return fmt.Sprintf("tvdb_show:%s:s%d", id, season)
	case "tmdb":
		if season < 0 {
			return "tmdb_movie:" + id
		}
		return fmt.Sprintf("tmdb_show:%s:s%d", id, season)
	case "imdb":
		if season < 0 {
			return "imdb_movie:" + id
		}
		return fmt.Sprintf("imdb_show:%s:s%d", id, season)
	default:
		return ""
	}
}

func mapEpisode(ranges map[string]string, episode int) (int, bool, error) {
	if episode < 1 {
		return 0, false, nil
	}
	if len(ranges) == 0 {
		return episode, true, nil
	}
	for source, target := range ranges {
		start, end, err := parseRange(source)
		if err != nil {
			return 0, false, err
		}
		if episode < start || (end > 0 && episode > end) {
			continue
		}
		return projectEpisode(target, episode-start)
	}
	return 0, false, nil
}

func parseRange(value string) (int, int, error) {
	parts := strings.SplitN(strings.TrimSpace(value), "-", 2)
	start, err := strconv.Atoi(parts[0])
	if err != nil || start < 1 {
		return 0, 0, fmt.Errorf("invalid range %q", value)
	}
	if len(parts) == 1 || parts[1] == "" {
		return start, 0, nil
	}
	end, err := strconv.Atoi(parts[1])
	if err != nil || end < start {
		return 0, 0, fmt.Errorf("invalid range %q", value)
	}
	return start, end, nil
}

func projectEpisode(spec string, sourceOffset int) (int, bool, error) {
	parts := strings.SplitN(spec, "|", 2)
	ratio := 1
	if len(parts) == 2 {
		var err error
		ratio, err = strconv.Atoi(parts[1])
		if err != nil || ratio == 0 {
			return 0, false, fmt.Errorf("invalid target ratio %q", spec)
		}
	}
	segments := strings.Split(parts[0], ",")
	values := make([]int, 0)
	for _, segment := range segments {
		start, end, err := parseRange(segment)
		if err != nil {
			return 0, false, err
		}
		if end == 0 {
			if ratio > 0 {
				return start + (sourceOffset+1)*ratio - 1, true, nil
			}
			return start + sourceOffset/(-ratio), true, nil
		}
		for n := start; n <= end; n++ {
			values = append(values, n)
		}
	}
	index := sourceOffset
	if ratio > 0 {
		index = (sourceOffset+1)*ratio - 1
	} else {
		index = sourceOffset / (-ratio)
	}
	if index < 0 || index >= len(values) {
		return 0, false, nil
	}
	return values[index], true, nil
}
