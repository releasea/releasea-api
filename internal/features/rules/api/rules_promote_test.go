package rules

import (
	"testing"

	"go.mongodb.org/mongo-driver/bson"
)

func TestNextRuleRevisionUsesHighestSibling(t *testing.T) {
	got := nextRuleRevision(bson.M{"revision": 2}, []bson.M{
		{"revision": 1},
		{"revision": 4},
	})
	if got != 5 {
		t.Fatalf("expected revision 5, got %d", got)
	}
}

func TestRulePublicationFlagsKeepExternalDistinct(t *testing.T) {
	internal, external := rulePublicationFlags([]string{
		"istio-system/releasea-internal-gateway",
		"istio-system/releasea-external-gateway",
	})
	if !internal || !external {
		t.Fatalf("expected both zones, internal=%v external=%v", internal, external)
	}
}
