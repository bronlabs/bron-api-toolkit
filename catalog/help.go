package catalog

type HelpTopic struct {
	Topic   string
	Title   string
	Details string
}

var HelpTopics = []HelpTopic{
	{
		Topic: "transaction-amounts",
		Title: "Reading transaction amounts and settlement",
		Details: "`params.amount` on a transaction is the requested amount, not the settled one. " +
			"For financial totals call bron_tx_list with `includeEvents: true` (or bron_tx_events for one " +
			"transaction) and aggregate `_embedded.events` — never sum `params.amount`.",
	},
	{
		Topic: "query-dates",
		Title: "Date filters and timestamps",
		Details: "Date query params accept ISO-8601 (`2026-04-01`, `2026-04-01T00:00:00Z`), raw epoch-millis, " +
			"or relative past forms `now`, `now-7d`, `-24h` (units s/m/h/d/w). Response timestamps come back as " +
			"ISO-8601 UTC.",
	},
	{
		Topic: "shaping-output",
		Title: "Trimming and reshaping tool responses",
		Details: "Pass `fields` (comma-separated dot-paths, e.g. `transactionId,params.amount`) to keep only those " +
			"paths, then `jq` for further filtering/aggregation. Both run server-side before the reply returns.",
	},
	{
		Topic: "pagination",
		Title: "List pagination and completeness",
		Details: "List responses carry `returned`, `limit` and `hasMore` under the envelope's `_embedded`, next to " +
			"the items array; they survive `fields` projection. `hasMore: true` means the page is NOT the whole " +
			"set — repeat with `offset` advanced by `limit` (or narrow the filters) before summarizing. A `jq` " +
			"program replaces the whole reply, so read `._embedded.hasMore` inside it when the signal still matters.",
	},
	{
		Topic: "recipes",
		Title: "Ready-made fields+jq patterns for common questions",
		Details: "`fields` paths are relative to each list item (the envelope is unwrapped), `jq` sees the full " +
			"envelope. Aggregations pass an explicit high limit (999 max) and carry `._embedded.hasMore` into the " +
			"result — true means the number covers only the first page. " +
			"Top holdings by USD value: bron_balances_list {nonEmpty: true, embed: \"prices\", limit: 999, jq: '" +
			recipeTopHoldingsJq + "'}. " +
			"Portfolio total USD: bron_balances_list {nonEmpty: true, embed: \"prices\", limit: 999, jq: '" +
			recipePortfolioTotalJq + "'}. " +
			"Stuck approvals: bron_tx_list {transactionStatuses: \"waiting-approval\", sortBy: \"updated\", sortDirection: \"ASC\", " +
			"fields: \"transactionId,transactionType,params.amount,createdAt\"}. " +
			"Net USD flow over 7 days: bron_tx_list {includeEvents: true, terminatedAtFrom: \"now-7d\", " +
			"transactionStatuses: \"completed,partially-completed\", limit: 999, jq: '" + recipeNetUsdFlowJq + "'} " +
			"— rewards count as inflow, fees as outflow; approval and staking-movement events are excluded. " +
			"In the portfolio total, priced < total means some balances had no USD price.",
	},
}

const (
	recipeTopHoldingsJq = `[.balances[] | select(._embedded.usdValue != null)] | sort_by(._embedded.usdValue | tonumber) | reverse | .[:10] | map({accountId, assetId, totalBalance, usdValue: ._embedded.usdValue})`

	recipePortfolioTotalJq = `{totalUsd: [.balances[] | (._embedded.usdValue // "0") | tonumber] | add, priced: [.balances[] | select(._embedded.usdValue != null)] | length, total: (.balances | length), hasMore: ._embedded.hasMore}`

	recipeNetUsdFlowJq = `{netUsd: [.transactions[]._embedded.events[]? | select(.usdAmount != null) | (.usdAmount | tonumber) * (if .eventType | IN("in","nft-in","stake-earn-reward","stake-reward-accrued","loyalty-reward","canton-reward") then 1 elif .eventType | IN("out","fee","nft-out","negative-deposit") then -1 else 0 end)] | add, hasMore: ._embedded.hasMore}`
)

func HelpTopicByName(topic string) (HelpTopic, bool) {
	for _, e := range HelpTopics {
		if e.Topic == topic {
			return e, true
		}
	}
	return HelpTopic{}, false
}
