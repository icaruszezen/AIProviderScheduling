package registry

import "testing"

func TestGetAvailableModelsForClientsHonorsScopedSuspension(t *testing.T) {
	const (
		modelID = "group-scoped-model"
		inside  = "group-inside"
		outside = "group-outside"
	)
	modelRegistry := newTestModelRegistry()
	modelRegistry.RegisterClient(inside, "codex", []*ModelInfo{{ID: modelID, OwnedBy: "group"}})
	modelRegistry.RegisterClient(outside, "codex", []*ModelInfo{{ID: "outside-model"}})
	modelRegistry.RegisterClient("suspended-peer", "codex", []*ModelInfo{{ID: modelID}})
	modelRegistry.SuspendClientModel("suspended-peer", modelID, "manual")

	models := modelRegistry.GetAvailableModelsForClients("openai", []string{inside, outside})
	if len(models) != 2 {
		t.Fatalf("models = %#v, want group model and outside model", models)
	}
	ids := map[string]struct{}{}
	for _, model := range models {
		id, _ := model["id"].(string)
		ids[id] = struct{}{}
	}
	if _, ok := ids[modelID]; !ok {
		t.Fatalf("missing %s in %#v", modelID, models)
	}
	if _, ok := ids["outside-model"]; !ok {
		t.Fatalf("missing outside-model in %#v", models)
	}

	hidden := modelRegistry.GetAvailableModelsForClients("openai", []string{"suspended-peer"})
	if len(hidden) != 0 {
		t.Fatalf("suspended client models = %#v, want none", hidden)
	}

	modelRegistry.SetModelQuotaExceeded(inside, modelID)
	quota := modelRegistry.GetAvailableModelsForClients("openai", []string{inside})
	if len(quota) != 1 {
		t.Fatalf("quota cooldown models = %#v, want the model to stay listed", quota)
	}
}
