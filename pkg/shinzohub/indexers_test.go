package shinzohub

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFetchIndexerPeers_PaginatesAndFilters(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/shinzonetwork/indexer/v1/indexers", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		assert.Equal(t, "1", r.URL.Query().Get("source_chain_id"))
		if r.URL.Query().Get("pagination.key") == "" {
			_, _ = w.Write([]byte(`{"indexers":[
				{"registered":true,"connection_string":"/ip4/1.1.1.1/tcp/9171/p2p/A","source_chain_id":"1"},
				{"registered":false,"connection_string":"/ip4/2.2.2.2/tcp/9171/p2p/B","source_chain_id":"1"},
				{"registered":true,"connection_string":"","source_chain_id":"1"},
				{"registered":true,"connection_string":"/ip4/4.4.4.4/tcp/9171/p2p/D","source_chain_id":"137"}
			],"pagination":{"next_key":"page2"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"indexers":[
			{"registered":true,"connection_string":" /ip4/3.3.3.3/tcp/9171/p2p/C "}
		],"pagination":{"next_key":""}}`))
	}))
	defer srv.Close()

	c := NewRPCClient(srv.URL, nil)
	peers, err := c.FetchIndexerPeers(context.Background(), 1)
	require.NoError(t, err)
	assert.Equal(t, []string{"/ip4/1.1.1.1/tcp/9171/p2p/A", "/ip4/3.3.3.3/tcp/9171/p2p/C"}, peers)
}

func TestFetchIndexerPeers_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	_, err := NewRPCClient(srv.URL, nil).FetchIndexerPeers(context.Background(), 1)
	assert.ErrorIs(t, err, ErrLCDHTTPStatus)
}

func TestFetchIndexerPeers_NotConfigured(t *testing.T) {
	_, err := NewRPCClient("", nil).FetchIndexerPeers(context.Background(), 1)
	assert.ErrorIs(t, err, ErrLCDNotConfigured)
}
