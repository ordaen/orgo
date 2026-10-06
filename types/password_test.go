package types

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

func TestNewPassword(t *testing.T) {
	p, err := NewPassword("secret")
	require.NoError(t, err)
	assert.Equal(t, "crypt", p.parts().Meth)
	assert.False(t, p.NeedsRehash())
	assert.True(t, p.Valid("secret"))
	assert.False(t, p.Valid("wrong"))
	p2, err := NewPassword("secret")
	require.NoError(t, err)
	assert.NotEqual(t, p, p2, "passwords are salted")

	_, err = NewPassword(strings.Repeat("a", 73))
	assert.ErrorIs(t, err, bcrypt.ErrPasswordTooLong)
}

func TestPasswordSSHA1(t *testing.T) {
	p := HashPassword("ssha1", "secret", "salt")
	assert.Equal(t, "salt", p.parts().salt())
	assert.True(t, p.Valid("secret"))
	assert.True(t, Password("{ssha1}"+string(p)).Valid("secret"))
	assert.False(t, p.Valid("wrong"))
	assert.True(t, p.NeedsRehash())
}

func TestPasswordUnknownMethod(t *testing.T) {
	assert.Empty(t, HashPassword("md5", "secret", ""))
	assert.False(t, Password("{md5}secret").Valid("secret"))
}

func TestPasswordCrypt(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("secret"), bcrypt.MinCost)
	require.NoError(t, err)
	p := Password("{crypt}" + string(hash))
	assert.True(t, p.Valid("secret"))
	assert.False(t, p.Valid("wrong"))
}
