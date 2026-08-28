package host

import (
	"hash/fnv"
	"math/rand"
	"net"
	"strings"

	ma "github.com/multiformats/go-multiaddr"
	"github.com/shinzonetwork/shinzo-host-client/pkg/logger"
)

// selectIndexerPeers decides which indexers this host replicates from.
//
// Every indexer peer pushes a full copy of every block, so the cost of
// ingestion scales linearly with the number of indexer peers. The host
// therefore never dials "everyone the hub knows"; it takes:
//
//  1. every explicitly configured peer (operator intent always wins), then
//  2. hub-discovered indexers, minus self and unroutable addresses, in a
//     stable pseudo-random order keyed by this node's peer ID (so different
//     hosts spread across different indexers instead of all picking the same),
//
// and stops at max. Entries are deduplicated in order.
func selectIndexerPeers(configured, discovered []string, selfPeerID string, max int) []string {
	if max < 1 {
		max = 1
	}

	seen := make(map[string]struct{})
	var out []string
	add := func(p string) bool {
		p = strings.TrimSpace(p)
		if p == "" {
			return false
		}
		if _, dup := seen[p]; dup {
			return false
		}
		seen[p] = struct{}{}
		out = append(out, p)
		return true
	}

	for _, p := range configured {
		add(p)
	}

	var candidates []string
	for _, p := range discovered {
		reason := rejectDiscoveredPeer(p, selfPeerID)
		if reason != "" {
			logger.Sugar.Debugf("Skipping hub indexer %s: %s", p, reason)
			continue
		}
		candidates = append(candidates, p)
	}
	stableShuffle(candidates, selfPeerID)

	for _, p := range candidates {
		if len(out) >= max {
			break
		}
		add(p)
	}

	if skipped := len(configured) + len(candidates) - len(out); skipped > 0 {
		logger.Sugar.Infof("Selected %d indexer peer(s) (max_indexer_peers=%d); %d candidate(s) not used", len(out), max, skipped)
	}
	return out
}

// rejectDiscoveredPeer returns a reason to skip a hub-advertised peer, or "".
func rejectDiscoveredPeer(addr, selfPeerID string) string {
	if isNodeURL(addr) {
		return "" // resolved later; nothing to inspect yet
	}
	maddr, err := ma.NewMultiaddr(addr)
	if err != nil {
		return "not a multiaddr"
	}
	if id, err := maddr.ValueForProtocol(ma.P_P2P); err == nil && selfPeerID != "" && id == selfPeerID {
		return "this node"
	}
	for _, proto := range []int{ma.P_IP4, ma.P_IP6} {
		v, err := maddr.ValueForProtocol(proto)
		if err != nil {
			continue
		}
		ip := net.ParseIP(v)
		if ip == nil || ip.IsLoopback() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() {
			return "unroutable address"
		}
	}
	return ""
}

// stableShuffle orders peers pseudo-randomly but deterministically for this
// node, so restarts keep the same indexer set while different nodes differ.
func stableShuffle(peers []string, seed string) {
	h := fnv.New64a()
	_, _ = h.Write([]byte(seed))
	r := rand.New(rand.NewSource(int64(h.Sum64()))) //nolint:gosec // not security-sensitive
	r.Shuffle(len(peers), func(i, j int) { peers[i], peers[j] = peers[j], peers[i] })
}
