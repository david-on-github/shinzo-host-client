package host

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

const (
	selfID = "12D3KooWBGsEwxj8XmsixETgGvWvKzMLvyMedPsMiX5J3LyEZdRn"
	peerA  = "/ip4/10.0.0.1/tcp/9171/p2p/12D3KooWSCL9mAcgmDDSYEFezA1Zdcbt9GQwXWyGdzUV73bTaMfm"
	peerB  = "/ip4/10.0.0.2/tcp/9171/p2p/12D3KooWExTcCjgaf5gM4YrGTEZKDCGW4ScMr2aZR9kHU4W1o61A"
	peerC  = "/ip4/10.0.0.3/tcp/9171/p2p/12D3KooWDUdHSCXBM5Wb7te6ZdWMgqddw7tJ7npWSzXK5tQgBsbT"
)

func TestSelectIndexerPeers_ConfiguredFirstThenCapped(t *testing.T) {
	got := selectIndexerPeers([]string{"https://node.example"}, []string{peerA, peerB, peerC}, selfID, 2)
	assert.Len(t, got, 2)
	assert.Equal(t, "https://node.example", got[0])
	assert.Contains(t, []string{peerA, peerB, peerC}, got[1])
}

func TestSelectIndexerPeers_SkipsSelfAndUnroutable(t *testing.T) {
	self := "/ip4/10.0.0.9/tcp/9171/p2p/" + selfID
	loop := "/ip4/127.0.0.1/tcp/9171/p2p/12D3KooWSCL9mAcgmDDSYEFezA1Zdcbt9GQwXWyGdzUV73bTaMfm"
	got := selectIndexerPeers(nil, []string{self, loop, peerA}, selfID, 10)
	assert.Equal(t, []string{peerA}, got)
}

func TestSelectIndexerPeers_StableAcrossRestarts(t *testing.T) {
	a := selectIndexerPeers(nil, []string{peerA, peerB, peerC}, selfID, 1)
	b := selectIndexerPeers(nil, []string{peerA, peerB, peerC}, selfID, 1)
	assert.Equal(t, a, b)
}

func TestSelectIndexerPeers_DifferentNodesSpread(t *testing.T) {
	picks := map[string]bool{}
	for _, id := range []string{"node-1", "node-2", "node-3", "node-4", "node-5", "node-6"} {
		picks[selectIndexerPeers(nil, []string{peerA, peerB, peerC}, id, 1)[0]] = true
	}
	assert.Greater(t, len(picks), 1, "different nodes should not all pick the same indexer")
}

func TestSelectIndexerPeers_ConfiguredAlwaysKeptEvenOverMax(t *testing.T) {
	got := selectIndexerPeers([]string{peerA, peerB, peerC}, []string{"/ip4/10.0.0.4/tcp/9171/p2p/12D3KooWSX99Hzxj8YdUGxKtbfvYxu29bE9kNzTwCMLTV7Kmg7EZ"}, selfID, 1)
	assert.Equal(t, []string{peerA, peerB, peerC}, got, "operator-configured peers are never dropped")
}

func TestSelectIndexerPeers_Dedup(t *testing.T) {
	got := selectIndexerPeers([]string{peerA}, []string{peerA, peerB}, selfID, 5)
	assert.Equal(t, []string{peerA, peerB}, got)
}
