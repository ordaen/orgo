package config

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// resetCustomVars removes the custom variables registered by the test.
func resetCustomVars(t *testing.T) {
	t.Cleanup(func() {
		customMu.Lock()
		clear(customVars)
		customMu.Unlock()
	})
}

func TestLoad(t *testing.T) {
	resetCustomVars(t)
	name := filepath.Join(t.TempDir(), ".env")
	require.NoError(t, os.WriteFile(name, []byte("ORGO_CFG_FILE=from-file\nCOOKIE_DOMAIN=example.com\n"), 0600))
	t.Cleanup(func() { os.Unsetenv("ORGO_CFG_FILE") })
	t.Setenv("COOKIE_DOMAIN", "")
	os.Unsetenv("COOKIE_DOMAIN")
	t.Setenv("LISTEN_ADDRESS", "127.0.0.1:9000")
	t.Setenv("EXTERNAL_RPC_IP", "10.1.2.3")
	Register("ORGO_CFG_FILE", "def")
	Register("ORGO_CFG_UNSET", "def")

	require.NoError(t, Load(name))
	exe, err := os.Executable()
	require.NoError(t, err)
	assert.Equal(t, filepath.Dir(filepath.Dir(exe)), AppRootDir)
	assert.Equal(t, filepath.Join(AppRootDir, "lib", "certs", "ca.crt"), CACert)
	assert.Equal(t, filepath.Join(AppRootDir, "lib", "cache.db"), CacheFile)
	assert.Equal(t, "127.0.0.1:9000", ListenAddress)
	assert.Equal(t, "example.com", CookieDomain)
	assert.Equal(t, "10.1.2.3", ExternalIP)
	assert.Equal(t, "from-file", Get("ORGO_CFG_FILE"), "registered before Load, set by the .env file")
	assert.Equal(t, "def", Get("ORGO_CFG_UNSET"))
}

func TestLoadMissingFile(t *testing.T) {
	assert.Error(t, Load(filepath.Join(t.TempDir(), "missing.env")))
}

func TestRegisterAfterLoad(t *testing.T) {
	resetCustomVars(t)
	t.Setenv("ORGO_CFG_LATE", "from-env")
	Register("ORGO_CFG_LATE", "def")
	assert.Equal(t, "from-env", Get("ORGO_CFG_LATE"))
}

func TestCustomVars(t *testing.T) {
	resetCustomVars(t)
	Register("ORGO_CFG_INT", "42")
	Register("ORGO_CFG_BOOL", "1")
	Register("ORGO_CFG_BAD", "x")
	assert.Equal(t, 42, GetInt("ORGO_CFG_INT"))
	assert.Equal(t, 0, GetInt("ORGO_CFG_BAD"))
	assert.Equal(t, 0, GetInt("ORGO_CFG_MISSING"))
	assert.True(t, GetBool("ORGO_CFG_BOOL"))
	assert.False(t, GetBool("ORGO_CFG_BAD"))
	assert.Equal(t, "", Get("ORGO_CFG_MISSING"))

	all := ShowAll()
	assert.Equal(t, "42", all["ORGO_CFG_INT"])
	all["ORGO_CFG_INT"] = "changed"
	assert.Equal(t, "42", Get("ORGO_CFG_INT"), "ShowAll returns a copy")
}

func TestCustomVarsConcurrent(t *testing.T) {
	resetCustomVars(t)
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			for range 100 {
				Register("ORGO_CFG_CONC", "v")
				Get("ORGO_CFG_CONC")
				ShowAll()
				loadCustomVars()
			}
		})
	}
	wg.Wait()
}
