package crypt

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewString(t *testing.T) {
	s := NewString("secret")

	assert.NotEqual(t, String("secret"), s)
	assert.NotContains(t, string(s), "secret")
	assert.Equal(t, s, NewString("secret"), "the same string encrypts to the same value")
	assert.NotEqual(t, s, NewString("secret2"))
	assert.Equal(t, "secret", s.Decode())
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
	assert.NotEqual(t, s, other)
	assert.Equal(t, "secret", other.Decode())
}

func TestMarshalJSON(t *testing.T) {
	b, err := json.Marshal(struct {
		Password String `json:"password"`
		Empty    String `json:"empty"`
	}{Password: NewString("secret")})
	require.NoError(t, err)
	assert.JSONEq(t, `{"password":"****","empty":"****"}`, string(b))
}

func TestUnmarshalJSON(t *testing.T) {
	var v struct {
		Password String `json:"password"`
		Empty    String `json:"empty"`
		Null     String `json:"null"`
		Masked   String `json:"masked"`
	}
	v.Null = NewString("old")
	v.Masked = NewString("old")

	require.NoError(t, json.Unmarshal([]byte(`{"password":"se\"creté","empty":"","null":null,"masked":"****"}`), &v))
	assert.Equal(t, "se\"creté", v.Password.Decode())
	assert.Equal(t, String(""), v.Empty)
	assert.Equal(t, "old", v.Null.Decode())
	assert.Equal(t, "old", v.Masked.Decode())
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

	b, err := json.Marshal(v)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(b, &v))
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

	require.NoError(t, s.UnmarshalGQL(""))
	assert.Equal(t, String(""), s)

	assert.Error(t, s.UnmarshalGQL(123))
}
