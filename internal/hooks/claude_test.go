package hooks

import "testing"

func TestMergeHookEntryKeepsOneCanopyHook(t *testing.T) {
	userHook := map[string]any{"hooks": []any{
		map[string]any{"type": "command", "command": "echo user"},
	}}
	oldCanopy := map[string]any{"hooks": []any{
		map[string]any{"type": "command", "command": "canopy _hook --session=old --event=Stop"},
	}}

	merged := mergeHookEntry([]any{userHook, oldCanopy}, "canopy _hook --session=$CANOPY_SESSION_ID --event=Stop")
	entries := merged.([]any)
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want user hook plus one canopy hook", len(entries))
	}
	if !isCanopyEntry(entries[1]) {
		t.Fatalf("last entry is not the canopy hook: %#v", entries[1])
	}
	if isCanopyEntry(entries[0]) {
		t.Fatal("user hook was incorrectly removed")
	}
}

func TestRemoveHookEntryOnlyRemovesLegacySessionHook(t *testing.T) {
	shared := map[string]any{"hooks": []any{
		map[string]any{"type": "command", "command": "canopy _hook --session=$CANOPY_SESSION_ID --event=Stop"},
	}}
	legacy := map[string]any{"hooks": []any{
		map[string]any{"type": "command", "command": "canopy _hook --session=old --event=Stop"},
	}}

	remaining, changed := removeHookEntry([]any{shared, legacy}, "old")
	entries := remaining.([]any)
	if !changed || len(entries) != 1 || !isCanopyEntry(entries[0]) {
		t.Fatalf("unexpected removal result: changed=%v entries=%#v", changed, entries)
	}
}
