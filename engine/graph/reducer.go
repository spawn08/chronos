package graph

// Reducer defines how a state value is combined when a node returns an update.
// The default behavior (nil reducer) is full replacement, but specific keys
// can be configured to append to slices, merge maps, etc.
type Reducer interface {
	Reduce(existing, update any) any
}

// ReducerFunc is a convenience adapter for functions.
type ReducerFunc func(existing, update any) any

func (f ReducerFunc) Reduce(existing, update any) any { return f(existing, update) }

// ReplaceReducer always replaces the existing value with the update.
// This is the default behavior when no reducer is configured.
type ReplaceReducer struct{}

func (ReplaceReducer) Reduce(_, update any) any { return update }

// AppendReducer appends update elements to an existing slice.
// If the existing value is not a slice, it is replaced.
// If the update is a single value, it is wrapped in a slice and appended.
type AppendReducer struct{}

func (AppendReducer) Reduce(existing, update any) any {
	existSlice, ok := existing.([]any)
	if !ok {
		existSlice = nil
		if existing != nil {
			existSlice = []any{existing}
		}
	}

	if updateSlice, ok := update.([]any); ok {
		return append(existSlice, updateSlice...)
	}
	return append(existSlice, update)
}

// MergeMapReducer performs a shallow merge of two maps. Update keys overwrite
// existing keys. Non-map values are replaced.
type MergeMapReducer struct{}

func (MergeMapReducer) Reduce(existing, update any) any {
	existMap, ok1 := existing.(map[string]any)
	updateMap, ok2 := update.(map[string]any)

	if !ok1 || !ok2 {
		return update
	}

	merged := make(map[string]any, len(existMap)+len(updateMap))
	for k, v := range existMap {
		merged[k] = v
	}
	for k, v := range updateMap {
		merged[k] = v
	}
	return merged
}

// StateSchema maps state keys to their reducers. Keys not in the schema
// use the default ReplaceReducer behavior.
type StateSchema map[string]Reducer

// ApplyUpdate merges an update into the existing state using the schema's reducers.
func (s StateSchema) ApplyUpdate(existing, update State) State {
	result := make(State, len(existing)+len(update))
	for k, v := range existing {
		result[k] = v
	}
	for k, v := range update {
		if reducer, ok := s[k]; ok && reducer != nil {
			result[k] = reducer.Reduce(result[k], v)
		} else {
			result[k] = v
		}
	}
	return result
}
