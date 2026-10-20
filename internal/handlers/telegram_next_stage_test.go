package handlers

import "testing"

func TestParseStockDecisionPayload(t *testing.T) {
	id, buyerChatID, err := parseStockDecisionData("conf_42_9001")
	if err != nil {
		t.Fatalf("expected valid stock decision payload, got error: %v", err)
	}
	if id != 42 || buyerChatID != "9001" {
		t.Fatalf("unexpected parsed values: id=%d buyerChatID=%s", id, buyerChatID)
	}
}
