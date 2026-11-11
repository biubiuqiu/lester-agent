package personal

import (
	"encoding/json"
	"strconv"
)

func memoryReferences(memories []Memory) []MemoryReference {
	refs := make([]MemoryReference, 0, len(memories))
	for _, m := range memories {
		refs = append(refs, MemoryReference{ID: m.ID, Revision: m.Revision, Topic: m.Topic, ProjectID: m.ProjectID})
	}
	return refs
}

// Project only the bounded public receipt. The stored usage array also holds
// provider accounting; it must never be returned wholesale to the manager.
func projectMemoryContext(raw []byte) []MemoryReference {
	var calls []struct {
		MemoryContext []MemoryReference `json:"memory_context"`
	}
	if json.Unmarshal(raw, &calls) != nil {
		return nil
	}
	seen := map[string]bool{}
	refs := []MemoryReference{}
	for _, call := range calls {
		for _, ref := range call.MemoryContext {
			if ref.ID == [16]byte{} || ref.Revision < 1 {
				continue
			}
			key := ref.ID.String() + ":" + strconv.FormatInt(ref.Revision, 10)
			if seen[key] {
				continue
			}
			seen[key] = true
			refs = append(refs, ref)
			if len(refs) == 36 {
				return refs
			}
		}
	}
	return refs
}
