package host

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"
)

// registrationPeerInfo is the subset of a node's /registration response needed
// to derive its libp2p address.
type registrationPeerInfo struct {
	P2P *struct {
		Self *struct {
			ID        string   `json:"id"`
			Addresses []string `json:"addresses"`
		} `json:"self"`
	} `json:"p2p"`
	Registration *struct {
		ConnectionString string `json:"connection_string"`
	} `json:"registration"`
}

// isNodeURL reports whether a bootstrap peer entry is an HTTP(S) node URL
// rather than a multiaddr or IP.
func isNodeURL(addr string) bool {
	lower := strings.ToLower(addr)
	return strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://")
}

// resolveNodeURL turns a node's HTTP URL (e.g. https://node.example or
// http://1.2.3.4:8080) into a fully-qualified libp2p multiaddr by asking the
// node for its own peer ID and P2P port via GET <url>/registration.
//
// This lets operators exchange a single address per node: the same URL serves
// the API for apps and, through this lookup, the P2P address for peers.
func resolveNodeURL(ctx context.Context, rawURL string, timeout time.Duration) (string, error) {
	if timeout <= 0 {
		timeout = DefaultPeerDiscoveryTimeout
	}

	u, err := url.Parse(strings.TrimRight(rawURL, "/"))
	if err != nil || u.Hostname() == "" {
		return "", fmt.Errorf("node URL %q: %w", rawURL, ErrInvalidNodeURL)
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String()+"/registration", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch %s/registration: %w", u, err)
	}
	defer func() { _ = resp.Body.Close() }()
	// 503 means "not ready" (no recent block), but the node still reports its
	// identity in the body, which is all we need to dial it.
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusServiceUnavailable {
		return "", fmt.Errorf("fetch %s/registration: %w (HTTP %d)", u, ErrNodeURLUnavailable, resp.StatusCode)
	}

	var info registrationPeerInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return "", fmt.Errorf("decode %s/registration: %w", u, err)
	}

	peerID, port := peerIDAndPortFromRegistration(info)
	if peerID == "" {
		return "", fmt.Errorf("%s/registration: %w", u, ErrNodeURLNoPeerID)
	}
	if _, err := peer.Decode(peerID); err != nil {
		return "", fmt.Errorf("%s/registration returned invalid peer ID %q: %w", u, peerID, err)
	}

	maddr, err := buildDNSOrIPMultiaddr(u.Hostname(), port)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s/p2p/%s", maddr, peerID), nil
}

// peerIDAndPortFromRegistration extracts the peer ID and TCP port a node
// advertises. It prefers the node's own connection_string, then falls back to
// the raw P2P self info.
func peerIDAndPortFromRegistration(info registrationPeerInfo) (peerID, port string) {
	port = DefaultP2PPort

	if info.Registration != nil && info.Registration.ConnectionString != "" {
		if maddr, err := ma.NewMultiaddr(info.Registration.ConnectionString); err == nil {
			if id, err := maddr.ValueForProtocol(ma.P_P2P); err == nil {
				peerID = id
			}
			if p, err := maddr.ValueForProtocol(ma.P_TCP); err == nil && p != "" {
				port = p
			}
		}
	}

	if info.P2P != nil && info.P2P.Self != nil {
		if peerID == "" {
			peerID = info.P2P.Self.ID
		}
		for _, a := range info.P2P.Self.Addresses {
			if maddr, err := ma.NewMultiaddr(a); err == nil {
				if p, err := maddr.ValueForProtocol(ma.P_TCP); err == nil && p != "" {
					port = p
					break
				}
			}
		}
	}

	return peerID, port
}

// buildDNSOrIPMultiaddr builds /ip4|ip6|dns4/<host>/tcp/<port>.
func buildDNSOrIPMultiaddr(host, port string) (ma.Multiaddr, error) {
	if net.ParseIP(host) != nil {
		return buildMultiaddr(host, port)
	}
	return ma.NewMultiaddr(fmt.Sprintf("/dns4/%s/tcp/%s", host, port))
}
