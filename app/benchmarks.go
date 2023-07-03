package app

import (
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
