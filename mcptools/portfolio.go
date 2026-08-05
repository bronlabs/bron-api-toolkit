package mcptools

import (
	"context"
	"fmt"
	"math/big"
	"sort"
	"strconv"
	"strings"

	"github.com/bronlabs/bron-api-toolkit/catalog"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const portfolioMaxPages = 20

func RegisterPortfolioSummary(server *mcp.Server, doer Doer, opts Options) {
	props := map[string]*jsonschema.Schema{
		"isTestnet": {
			Type:        "boolean",
			Description: "Keep only testnet holdings (true) or only mainnet ones (false). Omit for both.",
		},
		"groupBy": {
			Type:        "string",
			Description: "`asset` merges the same asset across accounts (default), `network` rolls up per network.",
		},
	}

	var required []string
	if opts.WorkspaceParam {
		props[WorkspaceParamName] = &jsonschema.Schema{Type: "string", Description: "Workspace to act in."}
		required = append(required, WorkspaceParamName)
	}

	tool := &mcp.Tool{
		Name: "bron_portfolio_summary",
		Description: "Total USD value of a workspace's holdings with the per-asset or per-network breakdown, " +
			"mainnet and testnet told apart, priced and unpriced positions counted separately. " +
			"Sums are decimal strings rounded at 18 fractional digits. " +
			"CLI mirror: `bron balances list --embed prices,networks`. Read-only.",
		InputSchema: &jsonschema.Schema{
			Type:                 "object",
			Properties:           props,
			Required:             required,
			AdditionalProperties: &jsonschema.Schema{},
		},
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}

	mcp.AddTool(server, tool, func(ctx context.Context, _ *mcp.CallToolRequest, in map[string]any) (*mcp.CallToolResult, any, error) {
		summary, err := portfolioSummary(ctx, doer, in, opts)
		if err != nil {
			return ErrorResult(err), nil, nil
		}

		return toolResult(summary)
	})
}

type portfolioPosition struct {
	AssetId   string `json:"assetId"`
	Symbol    string `json:"symbol,omitempty"`
	NetworkId string `json:"networkId,omitempty"`
	IsTestnet bool   `json:"isTestnet"`
	Balance   string `json:"totalBalance"`
	UsdPrice  string `json:"usdPrice,omitempty"`
	UsdValue  string `json:"usdValue,omitempty"`
	Accounts  int    `json:"accounts"`
}

type portfolioNetwork struct {
	NetworkId string `json:"networkId"`
	IsTestnet bool   `json:"isTestnet"`
	UsdValue  string `json:"usdValue"`
	Positions int    `json:"positions"`
	Unpriced  int    `json:"unpriced"`
}

func portfolioSummary(ctx context.Context, doer Doer, in map[string]any, opts Options) (map[string]any, error) {
	workspaceID := StringValue(in[WorkspaceParamName])

	wantTestnet, filtered, err := portfolioTestnetFilter(in)
	if err != nil {
		return nil, err
	}

	rows, truncated, err := fetchPortfolioBalances(ctx, doer, in, opts, wantTestnet, filtered)
	if err != nil {
		return nil, err
	}

	augmented := augmentorDoer(doer, in, opts)

	testnetByNetwork, err := FetchNetworkTestnetFlags(ctx, augmented, UniqueNetworkIds(rows))
	if err != nil {
		return nil, err
	}

	prices, pricesErr := FetchAssetPrices(ctx, augmented, UniqueAssetIds(rows))

	total := new(big.Rat)
	positions := 0
	accounts := map[string]bool{}
	byAsset := map[string]*portfolioPosition{}
	assetValues := map[string]*big.Rat{}
	assetBalances := map[string]*big.Rat{}
	byAssetAccounts := map[string]map[string]bool{}
	byNetwork := map[string]*portfolioNetwork{}
	networkTotals := map[string]*big.Rat{}

	var unpriced []string
	seenUnpriced := map[string]bool{}

	for _, balance := range balanceItems(rows) {
		assetID, _ := balance["assetId"].(string)
		networkID, _ := balance["networkId"].(string)
		isTestnet := testnetByNetwork[networkID]

		if filtered && isTestnet != wantTestnet {
			continue
		}

		amount, ok := new(big.Rat).SetString(numberAsString(balance["totalBalance"]))
		if assetID == "" || !ok {
			continue
		}

		positions++

		if accountID, _ := balance["accountId"].(string); accountID != "" {
			accounts[accountID] = true

			if byAssetAccounts[assetID] == nil {
				byAssetAccounts[assetID] = map[string]bool{}
			}
			byAssetAccounts[assetID][accountID] = true
		}

		position := byAsset[assetID]
		if position == nil {
			symbol, _ := balance["symbol"].(string)
			position = &portfolioPosition{
				AssetId:   assetID,
				Symbol:    symbol,
				NetworkId: networkID,
				IsTestnet: isTestnet,
			}
			byAsset[assetID] = position
			assetValues[assetID] = new(big.Rat)
			assetBalances[assetID] = new(big.Rat)
		}
		assetBalances[assetID].Add(assetBalances[assetID], amount)

		network := byNetwork[networkID]
		if network == nil {
			network = &portfolioNetwork{NetworkId: networkID, IsTestnet: isTestnet}
			byNetwork[networkID] = network
			networkTotals[networkID] = new(big.Rat)
		}
		network.Positions++

		price, priced := prices[assetID]
		if !priced || price.Price == "" {
			network.Unpriced++

			if !seenUnpriced[assetID] {
				seenUnpriced[assetID] = true
				unpriced = append(unpriced, assetID)
			}

			continue
		}

		rate, ok := new(big.Rat).SetString(price.Price)
		if !ok {
			continue
		}

		position.UsdPrice = price.Price

		value := new(big.Rat).Mul(amount, rate)
		assetValues[assetID].Add(assetValues[assetID], value)
		networkTotals[networkID].Add(networkTotals[networkID], value)
		total.Add(total, value)
	}

	sort.Strings(unpriced)

	totals := map[string]any{
		"usdValue":  trimDecimal(total.FloatString(18)),
		"accounts":  len(accounts),
		"assets":    len(byAsset),
		"positions": positions,
		"unpriced":  len(unpriced),
	}
	if truncated {
		totals["truncated"] = true
	}
	if pricesErr != nil {
		totals["pricesUnavailable"] = true
	}

	summary := map[string]any{
		"workspaceId":      workspaceID,
		"totals":           totals,
		"unpricedAssetIds": unpriced,
	}

	if filtered {
		summary["isTestnet"] = wantTestnet
	}

	if strings.EqualFold(StringValue(in["groupBy"]), "network") {
		summary["networks"] = networkRollup(byNetwork, networkTotals)
	} else {
		summary["assets"] = assetRollup(byAsset, byAssetAccounts, assetValues, assetBalances)
	}

	return summary, nil
}

func portfolioTestnetFilter(in map[string]any) (bool, bool, error) {
	raw := StringValue(in["isTestnet"])
	if raw == "" {
		return false, false, nil
	}

	want, err := strconv.ParseBool(raw)
	if err != nil {
		return false, false, fmt.Errorf("isTestnet must be true or false, got %q", raw)
	}

	return want, true, nil
}

func fetchPortfolioBalances(ctx context.Context, doer Doer, in map[string]any, opts Options, wantTestnet, filtered bool) ([]any, bool, error) {
	pathParams := map[string]string{}
	if opts.WorkspaceParam {
		pathParams[WorkspaceParamName] = StringValue(in[WorkspaceParamName])
	}

	var rows []any

	for page := 0; ; page++ {
		query := map[string]any{
			"nonEmpty": "true",
			"limit":    strconv.Itoa(maxMetaLimit),
			"offset":   strconv.Itoa(page * maxMetaLimit),
		}
		if filtered {
			query["isTestnet"] = strconv.FormatBool(wantTestnet)
		}

		var raw any
		if err := doer.Do(ctx, "GET", catalog.HelpEntries["balances"]["list"].Path, pathParams, nil, query, &raw); err != nil {
			return nil, false, err
		}

		batch := balanceItems(raw)
		for _, item := range batch {
			rows = append(rows, item)
		}

		if len(batch) < maxMetaLimit {
			return rows, false, nil
		}

		if page+1 >= portfolioMaxPages {
			return rows, true, nil
		}
	}
}

func assetRollup(byAsset map[string]*portfolioPosition, byAccounts map[string]map[string]bool, values, balances map[string]*big.Rat) []*portfolioPosition {
	out := make([]*portfolioPosition, 0, len(byAsset))
	for assetID, position := range byAsset {
		position.Accounts = len(byAccounts[assetID])
		position.Balance = trimDecimal(balances[assetID].FloatString(18))
		if position.UsdPrice != "" {
			position.UsdValue = trimDecimal(values[assetID].FloatString(18))
		}
		out = append(out, position)
	}

	sort.Slice(out, func(i, j int) bool {
		if cmp := values[out[j].AssetId].Cmp(values[out[i].AssetId]); cmp != 0 {
			return cmp < 0
		}

		return out[i].AssetId < out[j].AssetId
	})

	return out
}

func networkRollup(byNetwork map[string]*portfolioNetwork, totals map[string]*big.Rat) []*portfolioNetwork {
	out := make([]*portfolioNetwork, 0, len(byNetwork))
	for networkID, network := range byNetwork {
		network.UsdValue = trimDecimal(totals[networkID].FloatString(18))
		out = append(out, network)
	}

	sort.Slice(out, func(i, j int) bool { return out[i].NetworkId < out[j].NetworkId })

	return out
}

func trimDecimal(s string) string {
	if !strings.Contains(s, ".") {
		return s
	}

	return strings.TrimRight(strings.TrimRight(s, "0"), ".")
}
