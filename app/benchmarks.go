package app

import (
	"encoding/json"
	"fmt"
	"time"
)

// TODO : if connect cmd is used reset timer or simply don't allow benchark
// without bootstrap node
var startup_ts time.Time = time.Now()

// gathers all the benchmarks we want
type Benchmark struct {
	// time from startup to fully replicated
	Bootstrap time.Duration `json:"bootstrap"`

	// for each new contribution, that is not part of the bootstrapping process,
	// how long did it take to replicate
	NewContributions []time.Duration `json:"new-contribution"`

	// the region this node is working from
	Region string `json:"region"`
}

// custom json marshal
// TODO : doesn't seem like an ideal solution
func (b *Benchmark) MarshalJSON() ([]byte, error) {
	bootstrapSec := b.Bootstrap.Seconds()
	newContributionsSec := make([]float64, len(b.NewContributions))
	for i, c := range b.NewContributions {
		newContributionsSec[i] = c.Seconds()
	}

	// initialize variables to store the smallest, largest, and sum of elements
	smallest := 0.0
	largest := 0.0
	if len(newContributionsSec) > 0 {
		smallest = newContributionsSec[0]
		largest = newContributionsSec[0]
	}
	sum := 0.0

	// loop through the slice to calculate the smallest, largest, and sum of elements
	for _, val := range newContributionsSec {
		if val < smallest {
			smallest = val
		}
		if val > largest {
			largest = val
		}
		sum += val
	}

	average := 0.0
	if len(newContributionsSec) > 0 {
		average = sum / float64(len(newContributionsSec))
	}

	data := struct {
		BootrapSec float64 `json:"bootstrap"`
		AverageC   float64 `json:"averagec"`
		MinC       float64 `json:"minc"`
		MaxC       float64 `json:"maxc"`
		Region     string  `json:"region"`
	}{
		BootrapSec: bootstrapSec,
		AverageC:   average,
		MinC:       smallest,
		MaxC:       largest,
		Region:     b.Region,
	}

	return json.Marshal(data)
}

// bootstrapping-time is the time it takes a new node to gather all existing data.
// It is measured by checking each new replicated block for their timestamp.
// If their creation is previous to the creation of this node, the ellapsed time
// since startup is an "approximation"
func (b *Benchmark) UpdateBootstrap(ts time.Time) {
	if ts.Compare(startup_ts) <= 0 {
		fmt.Print("UpdateBootstrap")
		// replicated entry means we have a better approximation
		now := time.Now()
		b.Bootstrap = now.Sub(startup_ts)
	}
}

func (b *Benchmark) UpdateNewContributions(ts time.Time) {
	// if ts came before startup return
	if ts.Compare(startup_ts) < 0 {
		return
	}
	fmt.Print("UpdateNewContributions")

	// store the elapsed time between: a contribution has been added and it's
	// been replicated by this node
	now := time.Now()
	diff := now.Sub(ts)
	b.NewContributions = append(b.NewContributions, diff)
}
