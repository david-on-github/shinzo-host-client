package server

import (
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestConnectionString_AnnounceWins(t *testing.T) {
	r := httptest.NewRequest("GET", "http://203.0.113.9:8080/registration", nil)
	p2p := &P2PInfo{Self: &PeerInfo{ID: "12D3KooWTEST", Addresses: []string{"/ip4/0.0.0.0/tcp/9171"}}}

	assert.Equal(t, "/ip4/203.0.113.9/tcp/9171/p2p/12D3KooWTEST", deriveConnectionString(r, p2p), "no announce: derived from the request + listen port")

	p2p.Announce = "/dns4/node.example/tcp/19171"
	assert.Equal(t, "/dns4/node.example/tcp/19171/p2p/12D3KooWTEST", deriveConnectionString(r, p2p))

	p2p.Announce = " /ip4/198.51.100.7/tcp/9171/p2p/12D3KooWSTALE "
	assert.Equal(t, "/ip4/198.51.100.7/tcp/9171/p2p/12D3KooWTEST", deriveConnectionString(r, p2p), "stale /p2p/ suffix is replaced by the real id")
}
