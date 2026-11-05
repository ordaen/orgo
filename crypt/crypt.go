// Package crypt stores strings encrypted with AES-GCM, so they are encrypted at rest and hidden in GraphQL.
package crypt

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"strconv"
	"strings"
	"sync/atomic"
)

// Masked is the GraphQL value of every String. Unmarshaling it leaves the String unchanged,
// so a value sent back as it was received does not overwrite the secret.
const Masked = "****"

// LegacyPrefix starts the Strings of the legacy format, AES-CFB with a random IV, base64url encoded after the
// prefix. They are decoded with the key set by SetLegacyKey, new Strings are never written in this format.
const LegacyPrefix = ":rsa:"

// cipherKey is the default key, replace it with SetCipherKey in production.
var cipherKey = []byte("orgo-crypt-default-cipher-key!")

// keys holds the ciphers derived from the cipher key.
type keys struct {
	aead cipher.AEAD
	// nonce derives the nonce from the plain string, see NewDeterministicString.
	nonce func() hash.Hash
}

var (
	current atomic.Pointer[keys]
	legacy  atomic.Pointer[cipher.Block]
)

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

// SetLegacyKey sets the AES key, of 16, 24 or 32 bytes, that decodes the Strings of the legacy format,
// see LegacyPrefix. A nil key stops decoding them, they decode to an empty string.
func SetLegacyKey(key []byte) error {
	if key == nil {
		legacy.Store(nil)
		return nil
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return fmt.Errorf("crypt: legacy key: %w", err)
	}
	legacy.Store(&block)
	return nil
}

func deriveKey(key []byte, label string) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(label))
	return mac.Sum(nil)
}

// NewString creates a new encrypted String from a string with a random nonce, so the same string encrypts
// to a different String each time. An empty string is not encrypted and stays empty.
func NewString(s string) String {
	if s == "" {
		return ""
	}
	k := current.Load()
	nonce := make([]byte, k.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		panic(err) // crypto/rand never fails on the supported platforms
	}
	return seal(k.aead, nonce, s)
}

// NewDeterministicString creates a new encrypted String from a string, like NewString, but the nonce is an
// HMAC of the plain string, so the same string always encrypts to the same String and can be found with an
// equality query. It reveals which records hold equal values, but not the values themselves.
func NewDeterministicString(s string) String {
	if s == "" {
		return ""
	}
	k := current.Load()
	mac := k.nonce()
	mac.Write([]byte(s))
	return seal(k.aead, mac.Sum(nil)[:k.aead.NonceSize()], s)
}

func seal(aead cipher.AEAD, nonce []byte, s string) String {
	out := make([]byte, len(nonce), len(nonce)+len(s)+aead.Overhead())
	copy(out, nonce)
	return String(base64.StdEncoding.EncodeToString(aead.Seal(out, nonce, []byte(s), nil)))
}

// String is a wrapper around string. It stores the encrypted version of the string, base64 encoded.
type String string

// IsLegacy reports whether the String is in the legacy format, see LegacyPrefix. Encrypt it again with
// NewString(s.Decode()) to store it in the current format.
func (s String) IsLegacy() bool {
	return strings.HasPrefix(string(s), LegacyPrefix)
}

// Decode returns the decrypted string. It returns an empty string when the String is empty
// or can not be decrypted, like when it was encrypted with another key.
func (s String) Decode() string {
	plain, _ := s.decode()
	return plain
}

// decode returns the decrypted string and whether s is an encrypted String. A legacy String is always
// reported as encrypted, its format has no authentication to check.
func (s String) decode() (string, bool) {
	if s == "" {
		return "", false
	}
	if s.IsLegacy() {
		return decodeLegacy(strings.TrimPrefix(string(s), LegacyPrefix)), true
	}
	b, err := base64.StdEncoding.DecodeString(string(s))
	if err != nil {
		return "", false
	}
	gcm := current.Load().aead
	if len(b) < gcm.NonceSize() {
		return "", false
	}
	plain, err := gcm.Open(nil, b[:gcm.NonceSize()], b[gcm.NonceSize():], nil)
	if err != nil {
		return "", false
	}
	return string(plain), true
}

func decodeLegacy(s string) string {
	block := legacy.Load()
	if block == nil {
		return ""
	}
	b, err := base64.URLEncoding.DecodeString(s)
	if err != nil || len(b) < aes.BlockSize {
		return ""
	}
	plain := make([]byte, len(b)-aes.BlockSize)
	cipher.NewCFBDecrypter(*block, b[:aes.BlockSize]).XORKeyStream(plain, b[aes.BlockSize:]) //nolint:staticcheck // legacy format, decode only
	return string(plain)
}

// MarshalJSON implements the json.Marshaler interface. It writes the encrypted String, so a String
// stored in a JSON column, like a settings document, is not lost. GraphQL writes "****" instead.
func (s String) MarshalJSON() ([]byte, error) {
	return json.Marshal(string(s))
}

// UnmarshalJSON implements the json.Unmarshaler interface. An encrypted String, as written by MarshalJSON, is
// kept as it is, and any other string is encrypted. null and "****" leave the String unchanged.
func (s *String) UnmarshalJSON(b []byte) error {
	var v *string
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	if v == nil || *v == Masked {
		return nil
	}
	if _, ok := String(*v).decode(); ok {
		*s = String(*v)
		return nil
	}
	*s = NewString(*v)
	return nil
}

// MarshalGQL implements the graphql.Marshaler interface. The String is always written as "****".
func (s String) MarshalGQL(w io.Writer) {
	w.Write([]byte(strconv.Quote(Masked)))
}

// UnmarshalGQL implements the graphql.Unmarshaler interface. The value must be a plain string, it is always
// encrypted, unlike UnmarshalJSON, so an API client can not set an encrypted String it copied. "****" leaves
// the String unchanged.
func (s *String) UnmarshalGQL(v any) error {
	plain, ok := v.(string)
	if !ok {
		return errors.New("crypt.String must be a string")
	}
	if plain != Masked {
		*s = NewString(plain)
	}
	return nil
}
