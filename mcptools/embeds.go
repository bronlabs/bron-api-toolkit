package mcptools

import (
	"context"
	"encoding/json"
	"math/big"
	"strings"
)

// DefaultEmbedAugmentors is the shared `--embed` join surface: the augmentors
// every MCP consumer (bron-cli, Desktop) registers so `embed` tokens behave
// identically. Keyed by "<resource>.<verb>", matching Options.EmbedAugmentors.
var DefaultEmbedAugmentors = map[string]*EmbedAugmentor{
	"balances.list": {
		Description: "Comma-separated list of resolved/calculated extras to attach under `_embedded` per balance. Supported tokens: `prices` — fetches USD price + USD value (requires one extra REST call to /dictionary/asset-market-prices).",
		Apply:       applyBalancesPricesEmbed,
	},
	"tx.list": {
		Description: "Comma-separated list of resolved entities to attach under `_embedded` per transaction. Supported tokens: `assets` — resolves `params.assetId` to the full Asset DTO (symbol, networkId, decimals, ...) via one batch /dictionary/assets call.",
		Apply:       applyTxListAssetsEmbed,
	},
}

// applyTxListAssetsEmbed mirrors `wrapTxListEmbedAssets` for the MCP path:
// extracts assetIds from `params.assetId` of every transaction, batches one
// /dictionary/assets call, attaches the full Asset DTO under `_embedded.asset`
// per item. Soft-fails (returns nil) if the lookup blips so the agent still
// gets the bare list.
func applyTxListAssetsEmbed(ctx context.Context, doer Doer, result any, tokens []string) error {
	wantsAssets := false
	for _, t := range tokens {
		if t == "assets" {
			wantsAssets = true
			break
		}
	}
	if !wantsAssets {
		return nil
	}
	assetIds := UniqueTxAssetIds(result)
	if len(assetIds) == 0 {
		return nil
	}
	assetById, err := FetchAssetsById(ctx, doer, assetIds)
	if err != nil {
		return nil
	}
	EmbedAssetsIntoTxs(result, assetById)
	return nil
}

// applyBalancesPricesEmbed mirrors `wrapBalancesListEmbedPrices` for the MCP
// path: extracts assetIds from the balances response, fetches market prices
// in a single call, and merges `_embedded.{usdPrice, usdQuoteSymbolId,
// usdValue}` per item. Same helpers (`uniqueAssetIds`, `fetchAssetPrices`,
// `mergeBalancePrices`) as the CLI orchestrator — single source of truth.
//
// Soft-fails the price fetch — if the prices endpoint blips, the agent still
// gets the bare balances and can decide whether to retry.
func applyBalancesPricesEmbed(ctx context.Context, doer Doer, result any, tokens []string) error {
	wantsPrices := false
	for _, t := range tokens {
		if t == "prices" {
			wantsPrices = true
			break
		}
	}
	if !wantsPrices {
		return nil
	}
	assetIds := UniqueAssetIds(result)
	if len(assetIds) == 0 {
		return nil
	}
	priceByAsset, err := FetchAssetPrices(ctx, doer, assetIds)
	if err != nil {
		return nil
	}
	MergeBalancePrices(result, priceByAsset)
	return nil
}

// FetchAssetPrices returns a price-by-assetId map. /dictionary/asset-market-prices
// already echoes baseAssetId on each row, so a single call covers what used to
// take three (balances → assets list → symbol-market-prices) before the spec
// fix in libs/datamodel + platform/public-api.
func FetchAssetPrices(ctx context.Context, doer Doer, assetIds []string) (map[string]assetPrice, error) {
	var v interface{}
	query := map[string]interface{}{"baseAssetIds": strings.Join(assetIds, ",")}
	if err := doer.Do(ctx, "GET", "/dictionary/asset-market-prices", nil, nil, query, &v); err != nil {
		return nil, err
	}

	out := map[string]assetPrice{}
	m, ok := v.(map[string]interface{})
	if !ok {
		return out, nil
	}
	prices, _ := m["prices"].([]interface{})
	for _, item := range prices {
		pm, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		assetId, _ := pm["baseAssetId"].(string)
		quoteSymbolId, _ := pm["quoteSymbolId"].(string)
		price := numberAsString(pm["price"])
		if assetId == "" || price == "" {
			continue
		}
		out[assetId] = assetPrice{QuoteSymbolId: quoteSymbolId, Price: price}
	}
	return out, nil
}

type assetPrice struct {
	QuoteSymbolId string
	Price         string
}

// UniqueAssetIds walks a balances response and returns deduplicated assetIds.
// Treats both the wrapped `{"balances":[...]}` shape and a bare array as input.
func UniqueAssetIds(v interface{}) []string {
	seen := map[string]bool{}
	var out []string
	for _, b := range balanceItems(v) {
		if id, ok := b["assetId"].(string); ok && id != "" && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

func balanceItems(v interface{}) []map[string]interface{} {
	return mapItems(v, "balances")
}

// MergeBalancePrices mutates the balances response in place: each item gets
// `usdPrice`, `usdQuoteSymbolId`, and `usdValue = totalBalance * price` placed
// under `_embedded` so calculated fields don't pollute the spec-defined
// Balance shape. Same HATEOAS-style nesting that backend uses for resolved
// entities (e.g. WorkspaceMemberEmbedded). Multiplication uses big.Rat so
// trailing precision survives.
func MergeBalancePrices(v interface{}, prices map[string]assetPrice) {
	for _, b := range balanceItems(v) {
		assetId, _ := b["assetId"].(string)
		p, ok := prices[assetId]
		if !ok {
			continue
		}
		emb, _ := b["_embedded"].(map[string]interface{})
		if emb == nil {
			emb = map[string]interface{}{}
			b["_embedded"] = emb
		}
		emb["usdPrice"] = p.Price
		if p.QuoteSymbolId != "" {
			emb["usdQuoteSymbolId"] = p.QuoteSymbolId
		}
		total := numberAsString(b["totalBalance"])
		if total == "" {
			continue
		}
		if usdValue := mulDecimal(total, p.Price); usdValue != "" {
			emb["usdValue"] = usdValue
		}
	}
}

// numberAsString accepts the raw types `cli.Do` produces under UseNumber:
// json.Number values stringify to their wire form, plain strings pass through.
func numberAsString(v interface{}) string {
	switch t := v.(type) {
	case string:
		return t
	case json.Number:
		return string(t)
	}
	return ""
}

// mulDecimal multiplies two decimal strings without losing precision (the
// whole point of this CLI keeping decimals as strings). Result is rendered
// with up to 18 digits after the dot, trailing zeros trimmed; "" on parse
// failure so the merge step skips silently.
func mulDecimal(a, b string) string {
	ar, ok := new(big.Rat).SetString(a)
	if !ok {
		return ""
	}
	br, ok := new(big.Rat).SetString(b)
	if !ok {
		return ""
	}
	out := new(big.Rat).Mul(ar, br).FloatString(18)
	out = strings.TrimRight(out, "0")
	out = strings.TrimRight(out, ".")
	return out
}

func FetchAssetsById(ctx context.Context, doer Doer, assetIds []string) (map[string]map[string]interface{}, error) {
	var v interface{}
	query := map[string]interface{}{"assetIds": strings.Join(assetIds, ",")}
	if err := doer.Do(ctx, "GET", "/dictionary/assets", nil, nil, query, &v); err != nil {
		return nil, err
	}

	out := map[string]map[string]interface{}{}
	m, ok := v.(map[string]interface{})
	if !ok {
		return out, nil
	}
	arr, _ := m["assets"].([]interface{})
	for _, item := range arr {
		am, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		if id, _ := am["assetId"].(string); id != "" {
			out[id] = am
		}
	}
	return out, nil
}

// UniqueTxAssetIds collects every assetId reachable through `params.assetId` on
// each transaction. Intent transactions store fromAssetId/toAssetId in a
// separate intents resource, not on the tx itself, so they're not resolved
// here — that needs `--embed intents` (out of scope for this commit).
func UniqueTxAssetIds(v interface{}) []string {
	seen := map[string]bool{}
	var out []string
	for _, t := range txItems(v) {
		params, _ := t["params"].(map[string]interface{})
		if id, _ := params["assetId"].(string); id != "" && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

func txItems(v interface{}) []map[string]interface{} {
	return mapItems(v, "transactions")
}

// EmbedAssetsIntoTxs attaches the resolved Asset under `_embedded.asset` on
// each transaction whose `params.assetId` is in the map. Sticks to the
// existing TransactionEmbedded convention (`_embedded` already carries other
// resolved entities like accounts, events, signing requests).
func EmbedAssetsIntoTxs(v interface{}, assetById map[string]map[string]interface{}) {
	for _, t := range txItems(v) {
		params, _ := t["params"].(map[string]interface{})
		assetId, _ := params["assetId"].(string)
		asset, ok := assetById[assetId]
		if !ok {
			continue
		}
		emb, _ := t["_embedded"].(map[string]interface{})
		if emb == nil {
			emb = map[string]interface{}{}
			t["_embedded"] = emb
		}
		emb["asset"] = asset
	}
}

// mapItems unwraps a list-shape response (`{"<key>": [...]}`) or a bare array
// into a slice of object items. Used by --embed orchestrators that walk
// balances/tx/etc. without caring about the outer envelope.
func mapItems(v interface{}, key string) []map[string]interface{} {
	switch t := v.(type) {
	case map[string]interface{}:
		if arr, ok := t[key].([]interface{}); ok {
			return castMapSlice(arr)
		}
	case []interface{}:
		return castMapSlice(t)
	}
	return nil
}

func castMapSlice(arr []interface{}) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(arr))
	for _, item := range arr {
		if m, ok := item.(map[string]interface{}); ok {
			out = append(out, m)
		}
	}
	return out
}
