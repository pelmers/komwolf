package komwolf

import (
	"github.com/strava/go.strava"
	"log"
	"sort"
)

// KeyFunc returns a key for sorting a slice of segments.
type KeyFunc func(*strava.SegmentExplorerSegment) float64

// segmentKeySorter implements sort.Interface by key comparator.
type segmentKeySorter struct {
	segments []*strava.SegmentExplorerSegment
	key      KeyFunc
}

func (s segmentKeySorter) Len() int {
	return len(s.segments)
}

func (s segmentKeySorter) Less(i, j int) bool {
	return s.key(s.segments[i]) < s.key(s.segments[j])
}

func (s segmentKeySorter) Swap(i, j int) {
	s.segments[i], s.segments[j] = s.segments[j], s.segments[i]
}

// SortByKey sorts given segments in place using provided key function, ascending order.
func SortByKey(segments []*strava.SegmentExplorerSegment, key KeyFunc) {
	sort.Sort(segmentKeySorter{
		segments: segments,
		key:      key,
	})
}

// SortByDistance sorts given segments in place using segment distance as the key.
func SortByDistance(segments []*strava.SegmentExplorerSegment) {
	sort.Sort(segmentKeySorter{
		segments: segments,
		key: func(s *strava.SegmentExplorerSegment) float64 {
			return s.Distance
		},
	})
}

// deduplicate returns a copy of s with duplicates removed.
func deduplicate(s []*strava.SegmentExplorerSegment) []*strava.SegmentExplorerSegment {
	idSegments := make(map[int64]*strava.SegmentExplorerSegment)
	d := make([]*strava.SegmentExplorerSegment, 0, len(s))
	for _, val := range s {
		idSegments[val.Id] = val
	}
	for _, val := range idSegments {
		d = append(d, val)
	}
	return d
}

// SegmentLeader returns the leaderboard entry of the global leader of given segment id.
// If not found, return nil.
func SegmentLeader(client *strava.Client, id int64) *strava.SegmentLeaderboardEntry {
	segmentService := strava.NewSegmentsService(client)
	leaderboard := segmentService.GetLeaderboard(id)
	// Limit to top 1 of first page.
	result, err := leaderboard.PerPage(1).Page(1).Do()
	if err == nil && result.EntryCount > 0 {
		return result.Entries[0]
	}
	// I hate nil.
	return nil
}

// ExploreArea uses Strava explore API to find as many segments as possible in the
// provided area by recursive bisection up to given number of iterations.
// Activity type can be either "running" or "riding."
func ExploreArea(client *strava.Client, south, west, north, east float64,
	activityType string, iters int) []*strava.SegmentExplorerSegment {

	segments := make([]*strava.SegmentExplorerSegment, 0)
	segmentService := strava.NewSegmentsService(client)
	var recur func(s float64, w float64, n float64, e float64, i int)
	recur = func(s float64, w float64, n float64, e float64, i int) {
		explore := segmentService.Explore(s, w, n, e).ActivityType(activityType)
		exploreSegments, err := explore.Do()
		if err != nil {
			log.Printf("Explore failed at depth %d: %s", i, err.Error())
		}
		segments = append(segments, exploreSegments...)
		if i > 0 {
			// recursively subdivide into nw, ne, se, sw
			lonMid := e + (w-e)/2.0
			latMid := s + (n-s)/2.0
			recur(latMid, w, n, lonMid, i-1)
			recur(latMid, lonMid, n, e, i-1)
			recur(s, lonMid, latMid, e, i-1)
			recur(s, w, latMid, lonMid, i-1)
		}
	}
	recur(south, west, north, east, iters)
	return deduplicate(segments)
}
