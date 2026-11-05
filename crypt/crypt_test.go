package crypt

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// legacyKey and legacyValue are a test vector of the legacy format, made with the legacy code.
var (
	legacyKey   = []byte("orgo-legacy-test-key-24b")
	legacyValue = String(":rsa:HA4eoUFD5PQ0wO76hwSyUzMqflVdU8n4Xh2JWDY=")
)

func setLegacyKey(t *testing.T) {
	t.Helper()
	require.NoError(t, SetLegacyKey(legacyKey))
	t.Cleanup(func() { _ = SetLegacyKey(nil) })
}

func TestNewString(t *testing.T) {
	s := NewString("secret")

	assert.NotEqual(t, String("secret"), s)
	assert.NotContains(t, string(s), "secret")
	assert.NotEqual(t, s, NewString("secret"), "the nonce is random")
	assert.Equal(t, "secret", s.Decode())
	assert.False(t, s.IsLegacy())
}

func TestNewDeterministicString(t *testing.T) {
	s := NewDeterministicString("secret")

	assert.NotContains(t, string(s), "secret")
	assert.Equal(t, s, NewDeterministicString("secret"), "the same string encrypts to the same value")
	assert.NotEqual(t, s, NewDeterministicString("secret2"))
	assert.Equal(t, "secret", s.Decode())
	assert.Equal(t, String(""), NewDeterministicString(""))
}

func TestNewStringEmpty(t *testing.T) {
	assert.Equal(t, String(""), NewString(""))
	assert.Equal(t, "", String("").Decode())
}

func TestDecodeInvalid(t *testing.T) {
	assert.Equal(t, "", String("not base64!").Decode())
	assert.Equal(t, "", String("YWJj").Decode())
	assert.Equal(t, "", String("plain text that is long enough to have a nonce").Decode())
}

func TestSetCipherKey(t *testing.T) {
	t.Cleanup(func() { SetCipherKey(cipherKey) })

	s := NewString("secret")
	SetCipherKey([]byte("another key"))
	assert.Equal(t, "", s.Decode())

	other := NewString("secret")
	assert.Equal(t, "secret", other.Decode())
}

func TestLegacy(t *testing.T) {
	assert.True(t, legacyValue.IsLegacy())
	assert.Equal(t, "", legacyValue.Decode(), "no legacy key set")

	setLegacyKey(t)
	assert.Equal(t, "legacy secret", legacyValue.Decode())
	assert.Equal(t, "", String(LegacyPrefix+"not base64!").Decode())
	assert.Equal(t, "", String(LegacyPrefix+"YWJj").Decode())

	upgraded := NewString(legacyValue.Decode())
	assert.False(t, upgraded.IsLegacy())
	assert.Equal(t, "legacy secret", upgraded.Decode())

	require.NoError(t, SetLegacyKey(nil))
	assert.Equal(t, "", legacyValue.Decode())
}

func TestSetLegacyKeyInvalid(t *testing.T) {
	assert.Error(t, SetLegacyKey([]byte("short")))
}

func TestMarshalJSON(t *testing.T) {
	s := NewString("secret")
	b, err := json.Marshal(struct {
		Password String `json:"password"`
		Empty    String `json:"empty"`
	}{Password: s})
	require.NoError(t, err)
	assert.JSONEq(t, `{"password":"`+string(s)+`","empty":""}`, string(b))
}

func TestUnmarshalJSON(t *testing.T) {
	setLegacyKey(t)
	stored := NewString("stored")
	var v struct {
		Password String `json:"password"`
		Stored   String `json:"stored"`
		Legacy   String `json:"legacy"`
		Empty    String `json:"empty"`
		Null     String `json:"null"`
		Masked   String `json:"masked"`
	}
	v.Null = NewString("old")
	v.Masked = NewString("old")

	in := `{"password":"se\"creté","stored":"` + string(stored) + `","legacy":"` + string(legacyValue) +
		`","empty":"","null":null,"masked":"****"}`
	require.NoError(t, json.Unmarshal([]byte(in), &v))
	assert.Equal(t, "se\"creté", v.Password.Decode())
	assert.Equal(t, stored, v.Stored, "an encrypted String is kept")
	assert.Equal(t, legacyValue, v.Legacy, "a legacy String is kept")
	assert.Equal(t, "legacy secret", v.Legacy.Decode())
	assert.Equal(t, String(""), v.Empty)
	assert.Equal(t, "old", v.Null.Decode())
	assert.Equal(t, "old", v.Masked.Decode())
}

func TestUnmarshalJSONOtherKey(t *testing.T) {
	t.Cleanup(func() { SetCipherKey(cipherKey) })
	SetCipherKey([]byte("another key"))
	other := NewString("secret")
	SetCipherKey(cipherKey)

	var s String
	require.NoError(t, json.Unmarshal([]byte(`"`+string(other)+`"`), &s))
	assert.NotEqual(t, other, s, "a String of another key is not recognized, it is encrypted as a plain string")
	assert.Equal(t, string(other), s.Decode())
}

func TestUnmarshalJSONInvalid(t *testing.T) {
	var s String
	assert.Error(t, json.Unmarshal([]byte(`123`), &s))
	assert.Equal(t, String(""), s)
}

func TestJSONRoundTrip(t *testing.T) {
	v := struct {
		Password String `json:"password"`
	}{Password: NewString("secret")}
	want := v.Password

	b, err := json.Marshal(v)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(b, &v))
	assert.Equal(t, want, v.Password)
	assert.Equal(t, "secret", v.Password.Decode())
}

func TestMarshalGQL(t *testing.T) {
	var buf bytes.Buffer
	NewString("secret").MarshalGQL(&buf)
	assert.Equal(t, `"****"`, buf.String())
}

func TestUnmarshalGQL(t *testing.T) {
	var s String
	require.NoError(t, s.UnmarshalGQL("secret"))
	assert.Equal(t, "secret", s.Decode())

	require.NoError(t, s.UnmarshalGQL(Masked))
	assert.Equal(t, "secret", s.Decode())

	encrypted := NewString("other")
	require.NoError(t, s.UnmarshalGQL(string(encrypted)))
	assert.NotEqual(t, encrypted, s, "an encrypted String is encrypted again")
	assert.Equal(t, string(encrypted), s.Decode())

	require.NoError(t, s.UnmarshalGQL(""))
	assert.Equal(t, String(""), s)

	assert.Error(t, s.UnmarshalGQL(123))
}
