package mcptools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/bronlabs/bron-api-toolkit/catalog"
)

type portfolioStubDoer struct {
	balances string
	prices   string
	networks string
	paths    []string
}

func (d *portfolioStubDoer) Do(_ context.Context, _, path string, _ map[string]string, _, _, result any) error {
	d.paths = append(d.paths, path)

	var body string
	switch {
	case path == "/workspaces/{workspaceId}/balances":
		body = d.balances
	case strings.Contains(path, "asset-market-prices"):
		body = d.prices
	case strings.Contains(path, "networks"):
		body = d.networks
	}

	decoder := json.NewDecoder(strings.NewReader(body))
	decoder.UseNumber()

	return decoder.Decode(result)
}

func newPortfolioDoer() *portfolioStubDoer {
	return &portfolioStubDoer{
		balances: `{"balances":[
			{"accountId":"a1","assetId":"1","symbol":"BTC","networkId":"BTC","totalBalance":"0.1"},
			{"accountId":"a2","assetId":"1","symbol":"BTC","networkId":"BTC","totalBalance":"0.2"},
			{"accountId":"a1","assetId":"2","symbol":"USDT","networkId":"ETH","totalBalance":"10.005"},
			{"accountId":"a1","assetId":"3","symbol":"tBTC","networkId":"testBTC","totalBalance":"5"},
			{"accountId":"a1","assetId":"4","symbol":"SPAM","networkId":"ETH","totalBalance":"1000"}
		]}`,
		prices: `{"prices":[
			{"baseAssetId":"1","quoteSymbolId":"s01","price":"100000.005"},
			{"baseAssetId":"2","quoteSymbolId":"s01","price":"1.0001"},
			{"baseAssetId":"3","quoteSymbolId":"s01","price":"99999"}
		]}`,
		networks: `{"networks":[
			{"networkId":"BTC"},{"networkId":"ETH"},{"networkId":"testBTC","isTestnet":true}
		]}`,
	}
}

func summaryFor(t *testing.T, in map[string]any) map[string]any {
	t.Helper()

	doer := newPortfolioDoer()

	out, err := portfolioSummary(context.Background(), doer, in, Options{WorkspaceParam: true})
	if err != nil {
		t.Fatalf("portfolioSummary: %v", err)
	}

	if doer.paths[0] != catalog.HelpEntries["balances"]["list"].Path {
		t.Fatalf("balances path = %q", doer.paths[0])
	}

	return out
}

func totalsOf(t *testing.T, summary map[string]any) map[string]any {
	t.Helper()

	totals, ok := summary["totals"].(map[string]any)
	if !ok {
		t.Fatalf("totals missing from %v", summary)
	}

	return totals
}

func TestPortfolioSumsExactly(t *testing.T) {
	summary := summaryFor(t, map[string]any{"workspaceId": "w1", "isTestnet": false})

	if got := totalsOf(t, summary)["usdValue"]; got != "30010.0075005" {
		t.Fatalf("usdValue = %v", got)
	}
}

func TestPortfolioSeparatesTestnet(t *testing.T) {
	mainnet := totalsOf(t, summaryFor(t, map[string]any{"workspaceId": "w1", "isTestnet": false}))
	testnet := totalsOf(t, summaryFor(t, map[string]any{"workspaceId": "w1", "isTestnet": true}))

	if mainnet["assets"] != 3 {
		t.Fatalf("mainnet assets = %v, want the testnet one excluded", mainnet["assets"])
	}
	if testnet["usdValue"] != "499995" {
		t.Fatalf("testnet usdValue = %v", testnet["usdValue"])
	}
	if testnet["accounts"] != 1 {
		t.Fatalf("testnet accounts = %v", testnet["accounts"])
	}
	if testnet["positions"] != 1 || mainnet["positions"] != 4 {
		t.Fatalf("positions must be counted after the filter, got %v / %v", testnet["positions"], mainnet["positions"])
	}
}

func TestPortfolioAcceptsIsTestnetAsString(t *testing.T) {
	asString := totalsOf(t, summaryFor(t, map[string]any{"workspaceId": "w1", "isTestnet": "true"}))
	asBool := totalsOf(t, summaryFor(t, map[string]any{"workspaceId": "w1", "isTestnet": true}))

	if asString["usdValue"] != asBool["usdValue"] || asString["positions"] != asBool["positions"] {
		t.Fatalf("string form = %v, bool form = %v", asString, asBool)
	}

	if _, err := portfolioSummary(context.Background(), newPortfolioDoer(), map[string]any{"workspaceId": "w1", "isTestnet": "yes please"}, Options{WorkspaceParam: true}); err == nil {
		t.Fatal("an unparseable isTestnet must be an error, not a silently dropped filter")
	}
}

func TestPortfolioReturnsAnEmptyUnpricedList(t *testing.T) {
	doer := newPortfolioDoer()
	doer.balances = `{"balances":[{"accountId":"a1","assetId":"1","symbol":"BTC","networkId":"BTC","totalBalance":"0.1"}]}`

	summary, err := portfolioSummary(context.Background(), doer, map[string]any{"workspaceId": "w1"}, Options{WorkspaceParam: true})
	if err != nil {
		t.Fatalf("portfolioSummary: %v", err)
	}

	unpriced, ok := summary["unpricedAssetIds"].([]string)
	if !ok || unpriced == nil || len(unpriced) != 0 {
		t.Fatalf("unpricedAssetIds = %#v, want an empty list", summary["unpricedAssetIds"])
	}
}

func TestPortfolioCountsUnpricedSeparately(t *testing.T) {
	summary := summaryFor(t, map[string]any{"workspaceId": "w1"})

	if got := totalsOf(t, summary)["unpriced"]; got != 1 {
		t.Fatalf("unpriced = %v", got)
	}

	unpriced, _ := summary["unpricedAssetIds"].([]string)
	if len(unpriced) != 1 || unpriced[0] != "4" {
		t.Fatalf("unpricedAssetIds = %v", unpriced)
	}
}

func TestPortfolioMergesAnAssetHeldOnTwoAccounts(t *testing.T) {
	summary := summaryFor(t, map[string]any{"workspaceId": "w1", "isTestnet": false})

	assets, ok := summary["assets"].([]*portfolioPosition)
	if !ok || len(assets) == 0 {
		t.Fatalf("assets missing from %v", summary)
	}

	top := assets[0]
	if top.AssetId != "1" || top.Balance != "0.3" || top.Accounts != 2 {
		t.Fatalf("top position = %+v, want the merged BTC row", top)
	}
	if assets[len(assets)-1].AssetId != "4" {
		t.Fatalf("unpriced asset must sort last, got %+v", assets[len(assets)-1])
	}
}

func TestPortfolioGroupsByNetwork(t *testing.T) {
	summary := summaryFor(t, map[string]any{"workspaceId": "w1", "groupBy": "network"})

	if _, present := summary["assets"]; present {
		t.Fatal("groupBy=network must not also carry the per-asset list")
	}

	networks, ok := summary["networks"].([]*portfolioNetwork)
	if !ok || len(networks) != 3 {
		t.Fatalf("networks = %v", summary["networks"])
	}

	for _, network := range networks {
		if network.NetworkId == "ETH" && (network.UsdValue != "10.0060005" || network.Unpriced != 1) {
			t.Fatalf("ETH rollup = %+v", network)
		}
		if network.NetworkId == "testBTC" && !network.IsTestnet {
			t.Fatal("testBTC must be flagged as testnet")
		}
	}
}
