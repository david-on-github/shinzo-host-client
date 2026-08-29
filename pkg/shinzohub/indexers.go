package shinzohub

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// lcdIndexer mirrors the fields of the hub's indexer registry we care about.
type lcdIndexer struct {
	Registered       bool   `json:"registered"`
	ConnectionString string `json:"connection_string"`
	SourceChainID    string `json:"source_chain_id"` // LCD renders uint64 as a string
}

// FetchIndexerPeers returns the P2P connection strings of indexers registered
// on the hub for the given source chain. This is the network's live discovery
// list — the hub is the source of truth for who is indexing — but callers are
// expected to *select* from it (see host.selectIndexerPeers), not dial all of it.
func (c *RPCClient) FetchIndexerPeers(ctx context.Context, sourceChainID uint64) ([]string, error) {
	if c.lcdURL == "" {
		return nil, ErrLCDNotConfigured
	}

	const pageLimit = 100

	var peers []string
	nextKey := ""
	for {
		endpoint := fmt.Sprintf("%s/shinzonetwork/indexer/v1/indexers?pagination.limit=%d&source_chain_id=%d", c.lcdURL, pageLimit, sourceChainID)
		if nextKey != "" {
			endpoint += "&pagination.key=" + url.QueryEscape(nextKey)
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, fmt.Errorf("build LCD request: %w", err)
		}
		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("query indexers: %w", err)
		}

		var page struct {
			Indexers   []lcdIndexer `json:"indexers"`
			Pagination struct {
				NextKey string `json:"next_key"`
			} `json:"pagination"`
		}
		decodeErr := json.NewDecoder(resp.Body).Decode(&page)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("query indexers: %w (HTTP %d)", ErrLCDHTTPStatus, resp.StatusCode)
		}
		if decodeErr != nil {
			return nil, fmt.Errorf("decode indexers: %w", decodeErr)
		}

		for _, idx := range page.Indexers {
			cs := strings.TrimSpace(idx.ConnectionString)
			// The query filters server-side; re-check in case an older hub ignores the param.
			if idx.Registered && cs != "" && (idx.SourceChainID == "" || idx.SourceChainID == strconv.FormatUint(sourceChainID, 10)) {
				peers = append(peers, cs)
			}
		}

		if page.Pagination.NextKey == "" {
			return peers, nil
		}
		nextKey = page.Pagination.NextKey
	}
}
