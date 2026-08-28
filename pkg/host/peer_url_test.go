package host

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testPeerID = "12D3KooWBGsEwxj8XmsixETgGvWvKzMLvyMedPsMiX5J3LyEZdRn"

func registrationServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/registration", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
}

func TestResolveNodeURL_FromConnectionString(t *testing.T) {
	srv := registrationServer(t, `{"registration":{"connection_string":"/ip4/10.0.0.5/tcp/4001/p2p/`+testPeerID+`"}}`)
	defer srv.Close()

	got, err := resolveNodeURL(context.Background(), srv.URL, time.Second)
	require.NoError(t, err)

	u, _ := url.Parse(srv.URL)
	assert.Equal(t, "/ip4/"+u.Hostname()+"/tcp/4001/p2p/"+testPeerID, got)
}

func TestResolveNodeURL_FallsBackToP2PSelf(t *testing.T) {
	srv := registrationServer(t, `{"p2p":{"self":{"id":"`+testPeerID+`","addresses":["/ip4/0.0.0.0/tcp/9171"]}}}`)
	defer srv.Close()

	got, err := resolveNodeURL(context.Background(), srv.URL, time.Second)
	require.NoError(t, err)

	u, _ := url.Parse(srv.URL)
	assert.Equal(t, "/ip4/"+u.Hostname()+"/tcp/9171/p2p/"+testPeerID, got)
}

func TestResolveNodeURL_DNSHost(t *testing.T) {
	maddr, err := buildDNSOrIPMultiaddr("node.example", "9171")
	require.NoError(t, err)
	assert.Equal(t, "/dns4/node.example/tcp/9171", maddr.String())
}

func TestResolveNodeURL_NoPeerID(t *testing.T) {
	srv := registrationServer(t, `{"status":"ready"}`)
	defer srv.Close()

	_, err := resolveNodeURL(context.Background(), srv.URL, time.Second)
	assert.ErrorIs(t, err, ErrNodeURLNoPeerID)
}

func TestResolveNodeURL_NotReadyStillResolves(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"status":"not ready","p2p":{"self":{"id":"` + testPeerID + `","addresses":["/ip4/192.168.1.2/tcp/9171"]}}}`))
	}))
	defer srv.Close()

	got, err := resolveNodeURL(context.Background(), srv.URL, time.Second)
	require.NoError(t, err)
	u, _ := url.Parse(srv.URL)
	assert.Equal(t, "/ip4/"+u.Hostname()+"/tcp/9171/p2p/"+testPeerID, got)
}

func TestResolveNodeURL_Unavailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	_, err := resolveNodeURL(context.Background(), srv.URL, time.Second)
	assert.ErrorIs(t, err, ErrNodeURLUnavailable)
}

func TestResolveBootstrapPeers_MixedURLAndMultiaddr(t *testing.T) {
	srv := registrationServer(t, `{"registration":{"connection_string":"/ip4/10.0.0.5/tcp/9171/p2p/`+testPeerID+`"}}`)
	defer srv.Close()

	full := "/ip4/1.2.3.4/tcp/9171/p2p/" + testPeerID
	got := resolveBootstrapPeers(context.Background(), []string{srv.URL, full}, time.Second)

	u, _ := url.Parse(srv.URL)
	assert.Equal(t, []string{"/ip4/" + u.Hostname() + "/tcp/9171/p2p/" + testPeerID, full}, got)
}
