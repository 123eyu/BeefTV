package beefapi

import (
	"encoding/json"
	"infinite-canvas/backend/internal/workspace"
	"testing"
)

func TestH3CatalogPersistsCNYSecondPriceAndVideoProtocol(t *testing.T) {
	store, err := workspace.NewProviderConfig(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	entry := CatalogModel{ID: "minimax-h3-max-turbo", DisplayName: "MiniMax H3 Max Turbo", SupportedEndpointTypes: []string{"openai-video"}, VideoPricing: json.RawMessage(`{"currency":"CNY","mode":"seconds","rates":{"768p":{"output":0.2441304,"reference_video":0}}}`)}
	if err := applyCatalog(store, []CatalogModel{entry}, "", "42", ""); err != nil {
		t.Fatal(err)
	}
	profile := catalogProfile(t, store, entry.ID)
	if profile["capability"] != "video" || profile["protocol"] != "newapi-channel-2" {
		t.Fatalf("wrong profile: %#v", profile)
	}
	price, _ := profile["videoPricing"].(map[string]any)
	if price["currency"] != "CNY" || price["mode"] != "seconds" {
		t.Fatalf("CNY price lost: %#v", price)
	}
	entry.VideoPricing = nil
	if err := applyCatalog(store, []CatalogModel{entry}, "42", "42", ""); err != nil {
		t.Fatal(err)
	}
	if catalogProfile(t, store, entry.ID)["videoPricing"] != nil {
		t.Fatal("stale price survived catalog refresh")
	}
}
