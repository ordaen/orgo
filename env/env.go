// Package env reads the environment variables, with defaults for the unset ones.
package env

import (
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

// Load sets the environment variables of the .env file name. The variables already set are not changed.
func Load(name string) error {
	return godotenv.Load(name)
}

// Get returns the variable v, or def when it is not set or empty.
func Get(v, def string) string {
	r := os.Getenv(v)
	if r == "" {
		return def
	}
	return r
}

// GetInt returns the variable v as an int, or def when it is not set, empty or not an integer.
func GetInt(v string, def int) int {
	n, err := strconv.Atoi(os.Getenv(v))
	if err != nil {
		return def
	}
	return n
}

// GetBool returns the variable v as a bool, or def when it is not set, empty or not a boolean.
// The values accepted by strconv.ParseBool are booleans: 1, t, true, 0, f, false and their upper case forms.
func GetBool(v string, def bool) bool {
	b, err := strconv.ParseBool(os.Getenv(v))
	if err != nil {
		return def
	}
	return b
}
