// Package crypt stores strings encrypted with AES-GCM, so they are encrypted at rest and hidden in JSON.
package crypt

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"hash"
	"io"
	"strconv"
	"sync/atomic"
)

// Masked is the JSON and GraphQL value of every String. Unmarshaling it leaves the String unchanged,
// so a value sent back as it was received does not overwrite the secret.
const Masked = "****"

// cipherKey is the default key, replace it with SetCipherKey in production.
var cipherKey = []byte("orgo-crypt-default-cipher-key!")

// keys holds the ciphers derived from the cipher key.
type keys struct {
	aead cipher.AEAD
	// nonce derives the nonce from the plain string, see NewString.
	nonce func() hash.Hash
}

var current atomic.Pointer[keys]

func init() {
	SetCipherKey(cipherKey)
}

// SetCipherKey sets the cipher key. The key can be of any length, separate AES-256 and nonce keys
// are derived from it with HMAC-SHA256. Strings encrypted with another key can not be decoded after the change.
func SetCipherKey(key []byte) {
	block, err := aes.NewCipher(deriveKey(key, "orgo/crypt aes"))
	if err != nil {
		panic(err) // unreachable, a 32 byte key is always valid
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		panic(err)
	}
	nonceKey := deriveKey(key, "orgo/crypt nonce")
	current.Store(&keys{
		aead:  gcm,
		nonce: func() hash.Hash { return hmac.New(sha256.New, nonceKey) },
	})
}

func deriveKey(key []byte, label string) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(label))
	return mac.Sum(nil)
}

// NewString creates a new encrypted String from a string. An empty string is not encrypted and stays empty.
//
// The encryption is deterministic: the nonce is an HMAC of the plain string, so the same string
// always encrypts to the same String and can be found with an equality query. It reveals which
// records hold equal values, but not the values themselves.
func NewString(s string) String {
	if s == "" {
		return ""
	}
	k := current.Load()
	mac := k.nonce()
	mac.Write([]byte(s))
	nonce := mac.Sum(nil)[:k.aead.NonceSize()]
	out := make([]byte, len(nonce), len(nonce)+len(s)+k.aead.Overhead())
	copy(out, nonce)
	return String(base64.StdEncoding.EncodeToString(k.aead.Seal(out, nonce, []byte(s), nil)))
}

// String is a wrapper around string. It stores the encrypted version of the string, base64 encoded.
type String string

// Decode returns the decrypted string. It returns an empty string when the String is empty
// or can not be decrypted, like when it was encrypted with another key.
func (s String) Decode() string {
	if s == "" {
		return ""
	}
	b, err := base64.StdEncoding.DecodeString(string(s))
	if err != nil {
		return ""
	}
	gcm := current.Load().aead
	if len(b) < gcm.NonceSize() {
		return ""
	}
	plain, err := gcm.Open(nil, b[:gcm.NonceSize()], b[gcm.NonceSize():], nil)
	if err != nil {
		return ""
	}
	return string(plain)
}

// MarshalJSON implements the json.Marshaler interface. The String is always written as "****".
func (s String) MarshalJSON() ([]byte, error) {
	return []byte(`"` + Masked + `"`), nil
}

// UnmarshalJSON implements the json.Unmarshaler interface. It expects a plain string and encrypts it,
// null and "****" leave the String unchanged.
func (s *String) UnmarshalJSON(b []byte) error {
	var plain *string
	if err := json.Unmarshal(b, &plain); err != nil {
		return err
	}
	if plain != nil {
		s.set(*plain)
	}
	return nil
}

// MarshalGQL implements the graphql.Marshaler interface. The String is always written as "****".
func (s String) MarshalGQL(w io.Writer) {
	w.Write([]byte(strconv.Quote(Masked)))
}

// UnmarshalGQL implements the graphql.Unmarshaler interface. The value must be a string, it is encrypted,
// "****" leaves the String unchanged.
func (s *String) UnmarshalGQL(v any) error {
	plain, ok := v.(string)
	if !ok {
		return fmt.Errorf("crypt.String must be a string")
	}
	s.set(plain)
	return nil
}

// set encrypts the plain string into s, unless it is the Masked value.
func (s *String) set(plain string) {
	if plain != Masked {
		*s = NewString(plain)
	}
}
