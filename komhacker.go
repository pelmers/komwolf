package main

import (
	"flag"
	"fmt"
	"github.com/strava/go.strava"
	"gopkg.in/cheggaaa/pb.v1"
	"io/ioutil"
	"log"
	"os"
	"sort"
	"strconv"
	"strings"
)

var client *strava.Client

// Convert meters to either km or miles.
var convertMeters func(float64) float64

// "km" or "mi"
var distanceAbbrev string

// SegmentKeySorter implements sort.Interface by key comparator.
type SegmentKeySorter struct {
	segments []*strava.SegmentExplorerSegment
	key      func(*strava.SegmentExplorerSegment) float64
}

func (s SegmentKeySorter) Len() int {
	return len(s.segments)
}

func (s SegmentKeySorter) Less(i, j int) bool {
	return s.key(s.segments[i]) < s.key(s.segments[j])
}

func (s SegmentKeySorter) Swap(i, j int) {
	s.segments[i], s.segments[j] = s.segments[j], s.segments[i]
}

// Find the strava token either from environment or file.
func findToken() (string, error) {
	var contents []byte
	var err error
	tokenFilename := "STRAVA_TOKEN"
	// First check environment.
	val := os.Getenv(tokenFilename)
	if len(val) > 0 {
		return val, nil
	}
	// Not found in environment, check for a file.
	_, err = os.Stat(tokenFilename)
	if err == nil {
		contents, err = ioutil.ReadFile(tokenFilename)
		if err == nil {
			return string(contents), nil
		}
	}
	return "", err
}

func main() {
	swne := flag.String("b", "29.856, -95.593, 29.949, -95.139",
		"Comma-separated south west north east bounds")
	iters := flag.Int("i", 0, "Number of bisection iterations")
	metric := flag.Bool("m", false, "Use km for distance")
	detailFlag := flag.Bool("d", false, "Print leader pace on each found segment (may be slow)")
	activityType := flag.String("t", "running", "Activity type, either 'running' or 'riding'")
	accessToken, err := findToken()
	if err != nil {
		log.Fatal("Could not find a Strava public access token. Refer to README.")
	}
	client = strava.NewClient(accessToken)
	flag.Parse()
	if *metric {
		convertMeters = func(m float64) float64 { return m * 0.001 }
		distanceAbbrev = "km"
	} else {
		convertMeters = func(m float64) float64 { return m * 0.000621371 }
		distanceAbbrev = "mi"
	}
	swneSplit := strings.Split(*swne, ",")
	s, _ := strconv.ParseFloat(strings.TrimSpace(swneSplit[0]), 64)
	w, _ := strconv.ParseFloat(strings.TrimSpace(swneSplit[1]), 64)
	n, _ := strconv.ParseFloat(strings.TrimSpace(swneSplit[2]), 64)
	e, _ := strconv.ParseFloat(strings.TrimSpace(swneSplit[3]), 64)
	segments := deduplicate(bisectBounds(s, w, n, e, *activityType, *iters))
	log.Printf("Found %d unique segments", len(segments))
	sort.Sort(SegmentKeySorter{
		segments: segments,
		key: func(segment *strava.SegmentExplorerSegment) float64 {
			return segment.Distance
		},
	})
	details := make(map[int64]*strava.SegmentLeaderboardEntry)
	if *detailFlag {
		// Find all the segment details
		log.Print("Collecting segment leaderboards")
		bar := pb.StartNew(len(segments))
		for _, sm := range segments {
			if leader := segmentLeader(sm.Id); leader != nil {
				details[sm.Id] = leader
			}
			bar.Increment()
		}
		bar.Finish()
		// Sort by pace
		sort.Sort(SegmentKeySorter{
			segments: segments,
			key: func(segment *strava.SegmentExplorerSegment) float64 {
				if val, ok := details[segment.Id]; ok {
					return float64(val.ElapsedTime) / segment.Distance
				}
				return segment.Distance
			},
		})
	}
	for _, sm := range segments {
		printSegment(sm)
		if val, ok := details[sm.Id]; ok {
			printLeader(val, sm.Distance)
		}
	}
}

func printSegment(sm *strava.SegmentExplorerSegment) {
	prefix := "https://www.strava.com/segments"
	fmt.Printf("%s/%d %.2f %s: %s\n", prefix, sm.Id,
		convertMeters(sm.Distance), distanceAbbrev, sm.Name)
}

func printLeader(ld *strava.SegmentLeaderboardEntry, distance float64) {
	// find pace in minutes per mile
	minutes := float64(ld.ElapsedTime) / 60.0
	pace := minutes / convertMeters(distance)
	paceMinutes := int64(pace)
	// add 0.5 to round a positive number (math.Round does not exist)
	paceSeconds := int64((pace-float64(paceMinutes))*60.0 + 0.5)
	fmt.Printf("CR %d:%02d %s/min (%d s)\n", paceMinutes, paceSeconds,
		distanceAbbrev, ld.ElapsedTime)
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

func segmentLeader(id int64) *strava.SegmentLeaderboardEntry {
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

func bisectBounds(south, west, north, east float64,
	activityType string, iters int) []*strava.SegmentExplorerSegment {

	segments := make([]*strava.SegmentExplorerSegment, 0)
	segmentService := strava.NewSegmentsService(client)
	var recur func(s float64, w float64, n float64, e float64, i int)
	recur = func(s float64, w float64, n float64, e float64, i int) {
		explore := segmentService.Explore(s, w, n, e).ActivityType(activityType)
		log.Printf("Exploring %.3f, %.3f, %.3f, %.3f", s, w, n, e)
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
	return segments
}
