package config

import (
	"maps"
	"strconv"
	"sync"

	"github.com/ordaen/orgo/env"
)

var (
	customMu   sync.RWMutex
	customVars = map[string]string{}
)

// Register adds the custom variable key with the default value. The value of the environment variable key
// is used when it is set, also when Load runs later and loads it from the .env file.
func Register(key, value string) {
	customMu.Lock()
	defer customMu.Unlock()
	customVars[key] = env.Get(key, value)
}

// Get returns the value of the custom variable key, or "" when it is not registered.
func Get(key string) string {
	customMu.RLock()
	defer customMu.RUnlock()
	return customVars[key]
}

// GetInt returns the value of the custom variable key as an int, or 0 when it is not registered or not an integer.
func GetInt(key string) int {
	n, _ := strconv.Atoi(Get(key))
	return n
}

// GetBool returns the value of the custom variable key as a bool, see strconv.ParseBool.
// It returns false when it is not registered or not a boolean.
func GetBool(key string) bool {
	b, _ := strconv.ParseBool(Get(key))
	return b
}

// ShowAll returns a copy of the custom variables.
func ShowAll() map[string]string {
	customMu.RLock()
	defer customMu.RUnlock()
	return maps.Clone(customVars)
}

// loadCustomVars sets the custom variables to the values of the environment variables that are set.
func loadCustomVars() {
	customMu.Lock()
	defer customMu.Unlock()
	for k, v := range customVars {
		customVars[k] = env.Get(k, v)
	}
}
