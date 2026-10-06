package env

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGet(t *testing.T) {
	t.Setenv("ORGO_ENV_STR", "value")
	t.Setenv("ORGO_ENV_EMPTY", "")
	assert.Equal(t, "value", Get("ORGO_ENV_STR", "def"))
	assert.Equal(t, "def", Get("ORGO_ENV_EMPTY", "def"))
	assert.Equal(t, "def", Get("ORGO_ENV_UNSET", "def"))
}

func TestGetInt(t *testing.T) {
	for value, want := range map[string]int{"42": 42, "0": 0, "-3": -3, "": 7, "abc": 7, "1.5": 7} {
		t.Setenv("ORGO_ENV_INT", value)
		assert.Equal(t, want, GetInt("ORGO_ENV_INT", 7), "%q", value)
	}
}

func TestGetBool(t *testing.T) {
	for value, want := range map[string]bool{"true": true, "1": true, "TRUE": true, "false": false, "0": false, "": true, "yes": true} {
		t.Setenv("ORGO_ENV_BOOL", value)
		assert.Equal(t, want, GetBool("ORGO_ENV_BOOL", true), "%q", value)
	}
}

func TestLoad(t *testing.T) {
	name := filepath.Join(t.TempDir(), ".env")
	require.NoError(t, os.WriteFile(name, []byte("ORGO_ENV_LOADED=file\nORGO_ENV_SET=file\n"), 0600))
	t.Setenv("ORGO_ENV_SET", "env")
	t.Cleanup(func() { os.Unsetenv("ORGO_ENV_LOADED") })
	require.NoError(t, Load(name))
	assert.Equal(t, "file", Get("ORGO_ENV_LOADED", ""))
	assert.Equal(t, "env", Get("ORGO_ENV_SET", ""), "set variables are not changed")
	assert.Error(t, Load(filepath.Join(t.TempDir(), "missing")))
}
