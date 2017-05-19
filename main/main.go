package main

import (
	"flag"
	"fmt"
	"github.com/pelmers/komwolf"
	"github.com/strava/go.strava"
	"gopkg.in/cheggaaa/pb.v1"
	"io/ioutil"
	"log"
	"os"
	"strconv"
	"strings"
)

// Convert meters to either km or miles.
var convertMeters func(float64) float64

// "km" or "mi"
var distanceAbbrev string

// Find the strava token either from environment or file.
func findToken() (string, error) {
	var contents []byte
	var err error
	tokenFilename := "STRAVA_TOKEN"
	// First check environment.
	val := strings.TrimSpace(os.Getenv(tokenFilename))
	if len(val) > 0 {
		return val, nil
	}
	// Not found in environment, check for a file.
	_, err = os.Stat(tokenFilename)
	if err == nil {
		contents, err = ioutil.ReadFile(tokenFilename)
		if err == nil {
			return strings.TrimSpace(string(contents)), nil
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
	client := strava.NewClient(accessToken)
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
	// Sanity check: assert s < n and w < e.
	if s > n || w > e {
		log.Fatal("Provided bounds do not make sense. Please check.")
	}
	segments := komwolf.ExploreArea(client, s, w, n, e, *activityType, *iters)
	log.Printf("Found %d unique segments", len(segments))
	komwolf.SortByDistance(segments)
	details := make(map[int64]*strava.SegmentLeaderboardEntry)
	if *detailFlag {
		// Find all the segment details
		log.Print("Collecting segment leaderboards")
		bar := pb.StartNew(len(segments))
		for _, sm := range segments {
			if leader := komwolf.SegmentLeader(client, sm.Id); leader != nil {
				details[sm.Id] = leader
			}
			bar.Increment()
		}
		bar.Finish()
		// Use leader pace as key
		komwolf.SortByKey(segments, func(segment *strava.SegmentExplorerSegment) float64 {
			if val, ok := details[segment.Id]; ok {
				return float64(val.ElapsedTime) / segment.Distance
			}
			return segment.Distance
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
