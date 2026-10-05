package types

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"golang.org/x/crypto/bcrypt"
)

func TestNewPassword(t *testing.T) {
	p := NewPassword("secret")
	assert.True(t, p.Valid("secret"))
	assert.False(t, p.Valid("wrong"))
	assert.NotEqual(t, p, NewPassword("secret"), "passwords are salted")
}

func TestPasswordSSHA1(t *testing.T) {
	p := HashPassword("ssha1", "secret", "salt")
	assert.Equal(t, "salt", p.parts().salt())
	assert.True(t, p.Valid("secret"))
	assert.True(t, Password("{ssha1}"+string(p)).Valid("secret"))
	assert.False(t, p.Valid("wrong"))
}

func TestPasswordCrypt(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("secret"), bcrypt.MinCost)
	assert.NoError(t, err)
	p := Password("{crypt}" + string(hash))
	assert.True(t, p.Valid("secret"))
	assert.False(t, p.Valid("wrong"))
}
