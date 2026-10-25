package ice

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// After Restart the agent must keep the old path working for both directions until
// a pair of the new generation is selected: the peer that has not moved yet is
// still sending to the old addresses and still listening on them.
func TestRestartKeepsTheOldPathWorkingUntilANewPairIsSelected(t *testing.T) {
	cfg := &AgentConfig{NetworkTypes: supportedNetworkTypes()}
	aAgent, err := NewAgent(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, aAgent.Close()) })
	bAgent, err := NewAgent(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, bAgent.Close()) })

	aConn, bConn := connect(t, aAgent, bAgent)

	read := func(c *Conn, within time.Duration) (string, bool) {
		require.NoError(t, c.SetReadDeadline(time.Now().Add(within)))
		buf := make([]byte, 64)
		n, err := c.Read(buf)
		if err != nil {
			return "", false
		}

		return string(buf[:n]), true
	}

	_, err = aConn.Write([]byte("before"))
	require.NoError(t, err)
	got, ok := read(bConn, 2*time.Second)
	require.True(t, ok)
	require.Equal(t, "before", got)

	// B restarts. A has not heard about it yet and carries on over the old path.
	require.NoError(t, bAgent.Restart("", ""))

	_, err = aConn.Write([]byte("during"))
	require.NoError(t, err)
	got, ok = read(bConn, 2*time.Second)
	require.True(t, ok, "media sent on the old path was dropped by the restarting agent")
	require.Equal(t, "during", got)

	_, err = bConn.Write([]byte("outgoing"))
	require.NoError(t, err, "the restarting agent has nowhere to write until the new pair is selected")
	got, ok = read(aConn, 2*time.Second)
	require.True(t, ok, "what the restarting agent wrote on the old path never arrived")
	require.Equal(t, "outgoing", got)

	// Once the old generation is retired for good, its path is closed.
	require.NoError(t, bAgent.loop.Run(bAgent.loop, func(context.Context) { bAgent.closeRetired() }))
	_, err = aConn.Write([]byte("after"))
	require.NoError(t, err)
	_, ok = read(bConn, 500*time.Millisecond)
	require.False(t, ok, "the old path was still open after the old generation was closed")
}

// A restart while no pair was selected has nothing to retire and must not fail.
func TestRestartBeforeAnythingIsSelectedRetiresNothing(t *testing.T) {
	agent, err := NewAgent(&AgentConfig{NetworkTypes: supportedNetworkTypes()})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, agent.Close()) })
	require.NoError(t, agent.Restart("", ""))
	require.NoError(t, agent.Restart("", "")) // twice in a row
}
