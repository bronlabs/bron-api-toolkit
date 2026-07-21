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
}

func HelpTopicByName(topic string) (HelpTopic, bool) {
	for _, e := range HelpTopics {
		if e.Topic == topic {
			return e, true
		}
	}
	return HelpTopic{}, false
}
