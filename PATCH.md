# Patched copy of github.com/pion/ice/v4 (v4.4.8)

This directory is pion/ice v4.4.8 with one change, so that an ICE restart is
**make before break** as RFC 8445 section 9 intends. It is MIT licensed
(see LICENSE); the change is in `retire.go` plus six lines marked `PATCH` in
`agent.go` and `transport.go`.

## Why

Upstream `Agent.Restart` closes every local socket and forgets every remote
candidate immediately. A browser whose connection was healthy keeps sending on
its old path until it has switched to the new one, which takes seconds, and
everything sent in that time goes to a closed port. The loss also makes the
browser cut its bandwidth estimate, which then takes many seconds to recover.
Measured with real Chrome, restarting a healthy connection froze a viewer's video
for 3 to 10 seconds in 9 of 14 runs (the kernel counted about 500 datagrams
"dropped due to no socket" in each stalled run).

## What changed

On restart the old generation is *retired* instead of closed:

- its local sockets stay open and its remote candidates stay known, so media
  still arriving from the old path is read and accepted;
- its selected pair stays the pair application data (RTP, RTCP) is written to
  until a pair of the new generation is selected;
- it is closed 3 s after the new pair is selected, or 20 s after the restart if
  none ever is, or at once if another restart or a close comes first.

Connectivity checks, nomination and state reporting are untouched and use the new
generation only.

## Upstream

Worth proposing upstream. Until then, when updating pion/webrtc, re-apply this
patch to the matching pion/ice version.
