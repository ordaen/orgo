package types

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestVariables(t *testing.T) {
	vars := Variables{{Name: "a", Value: "1"}, {Name: "b", Value: "2"}}
	assert.Equal(t, 1, vars.IndexOf("b"))
	assert.Equal(t, -1, vars.IndexOf("c"))
	assert.True(t, vars.Contains("a"))
	assert.False(t, vars.Contains("c"))
	assert.Equal(t, map[string]any{"a": "1", "b": "2"}, vars.Map())
	assert.Equal(t, map[string]string{"a": "1", "b": "2"}, vars.StringMap())
}
