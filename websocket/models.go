package websocket

import "slices"

// subscribedModels holds the subscribed IDs by model name
type subscribedModels map[string][]string

func (mods subscribedModels) includes(kind, id string) bool {
	return slices.Contains(mods[kind], id)
}

// set replaces the subscribed IDs of the model
func (mods subscribedModels) set(kind string, ids ...string) {
	mods[kind] = ids
}
