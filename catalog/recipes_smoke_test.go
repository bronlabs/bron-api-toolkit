package catalog

import (
	"encoding/json"
	"testing"

	"github.com/bronlabs/bron-api-toolkit/jqfilter"
)

func mustUnmarshal(t *testing.T, data string) any {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(data), &v); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return v
}

func TestRecipeJqProgramsRun(t *testing.T) {
	tx := mustUnmarshal(t, `{"transactions":[{"_embedded":{"events":[{"eventType":"in","usdAmount":"100"},{"eventType":"stake-earn-reward","usdAmount":"10"},{"eventType":"fee","usdAmount":"1"},{"eventType":"allowance","usdAmount":"999"},{"eventType":"out","usdAmount":"50"}]}}],"_embedded":{"hasMore":false}}`)
	out, err := jqfilter.Run(recipeNetUsdFlowJq, tx)
	if err != nil {
		t.Fatalf("net flow: %v", err)
	}
	if got, _ := json.Marshal(out); string(got) != `{"hasMore":false,"netUsd":59}` {
		t.Fatalf("net flow got %s", got)
	}

	bal := mustUnmarshal(t, `{"balances":[{"_embedded":{"usdValue":"5"}},{}],"_embedded":{"hasMore":true}}`)
	out, err = jqfilter.Run(recipePortfolioTotalJq, bal)
	if err != nil {
		t.Fatalf("portfolio: %v", err)
	}
	if got, _ := json.Marshal(out); string(got) != `{"hasMore":true,"priced":1,"total":2,"totalUsd":5}` {
		t.Fatalf("portfolio got %s", got)
	}

	if _, err := jqfilter.Run(recipeTopHoldingsJq, bal); err != nil {
		t.Fatalf("top holdings: %v", err)
	}
}
