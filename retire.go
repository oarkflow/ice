package ice

import (
	"context"
	"net/netip"
	"sync/atomic"
	"time"
)

// This file is a patch to pion/ice (see PATCH.md in this directory).
//
// An ICE restart is meant to be make-before-break (RFC 8445 section 9): the
// peer that restarted keeps using the old selected pair until a pair of the new
// generation is selected, and the other side must keep the old path working
// until then. Upstream's Agent.Restart instead closes every local socket and
// forgets every remote candidate at once, so anything the peer sends in the
// meantime goes to a closed port and is lost, and what the agent wants to send
// has nowhere to go. A browser takes seconds to switch, and the loss makes it
// cut its bandwidth estimate, so a restart of a healthy connection froze video
// for 3 to 10 seconds.
//
// Here the old generation is retired instead of closed:
//   - its local sockets stay open, so media still arriving on them is read;
//   - its remote candidates stay known, so that media is accepted;
//   - its selected pair stays the pair application data is written to, until a
//     pair of the new generation is selected.
//
// It is closed shortly after the new pair is selected, or after retiredMaxAge
// if none ever is.
const (
	// retiredGrace is how long the old generation lives after the new pair is
	// selected, so packets already in flight on the old path are still read.
	retiredGrace = 3 * time.Second
	// retiredMaxAge is the longest the old generation lives if no new pair is
	// selected. A path that is really gone costs nothing but a few open sockets.
	retiredMaxAge = 20 * time.Second
)

type retiredGeneration struct {
	local  map[NetworkType][]Candidate
	remote map[NetworkType][]Candidate
	timer  *time.Timer
}

// retiredPair is the selected pair of the generation being retired: the one
// application data is still written to while no pair of the new generation is
// selected.
type retiredPairHolder struct{ p atomic.Pointer[CandidatePair] }

// retireCandidates moves the current candidates into the retired generation
// and leaves empty sets for the new one. It runs on the agent's loop.
func (a *Agent) retireCandidates() {
	a.closeRetired() // a restart while one is retired: the older one goes now
	r := &retiredGeneration{local: a.localCandidates, remote: a.remoteCandidates}
	a.localCandidates = make(map[NetworkType][]Candidate)
	a.remoteCandidates = make(map[NetworkType][]Candidate)
	r.timer = time.AfterFunc(retiredMaxAge, func() {
		_ = a.loop.Run(a.loop, func(context.Context) { a.closeRetired() })
	})
	a.retired = r
}

// closeRetired closes the retired generation, if there is one.
func (a *Agent) closeRetired() {
	r := a.retired
	if r == nil {
		return
	}
	a.retired = nil
	a.retiredPair.p.Store(nil)
	if r.timer != nil {
		r.timer.Stop()
	}
	for _, set := range []map[NetworkType][]Candidate{r.local, r.remote} {
		for _, cs := range set {
			for _, c := range cs {
				if err := c.close(); err != nil {
					a.log.Warnf("Failed to close retired candidate %s: %v", c, err)
				}
			}
		}
	}
}

// findRetiredRemote finds a remote candidate of the retired generation.
func (a *Agent) findRetiredRemote(networkType NetworkType, addr netip.AddrPort) Candidate {
	if a.retired == nil {
		return nil
	}
	for _, c := range a.retired.remote[networkType] {
		if addrPortEqual(c.addrPort(), addr) {
			return c
		}
	}

	return nil
}

// newPairSelected is called when a pair of the new generation is selected: the
// old path is no longer needed for writing, and is closed after a short grace.
func (a *Agent) newPairSelected() {
	if a.retired == nil {
		return
	}
	a.retiredPair.p.Store(nil)
	a.retired.timer.Stop()
	a.retired.timer = time.AfterFunc(retiredGrace, func() {
		_ = a.loop.Run(a.loop, func(context.Context) { a.closeRetired() })
	})
}
