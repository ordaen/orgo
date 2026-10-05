package types

import (
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base64"
	"io"
	"regexp"

	"golang.org/x/crypto/bcrypt"
)

type passwordParts struct {
	Meth string
	Pass string
}

func (t passwordParts) salt() string {
	switch t.Meth {
	case "ssha1":
		br, err := base64.StdEncoding.DecodeString(t.Pass)
		if err == nil && len(br) > 4 {
			return string(br[len(br)-4:])
		}
	}
	return ""
}

// Password type
type Password string

var passwordRe = regexp.MustCompile(`^\{(.+)\}(.+)$`)

func (t Password) parts() passwordParts {
	res := passwordRe.FindStringSubmatch(string(t))
	if len(res) > 2 {
		return passwordParts{Meth: res[1], Pass: res[2]}
	}
	return passwordParts{Meth: "ssha1", Pass: string(t)}
}

// Valid verifying password
func (t Password) Valid(s string) bool {
	ps := t.parts()
	switch ps.Meth {
	case "crypt":
		return bcrypt.CompareHashAndPassword([]byte(ps.Pass), []byte(s)) == nil
	case "ssha1":
		hp := HashPassword(ps.Meth, s, ps.salt()).parts()
		return subtle.ConstantTimeCompare([]byte(ps.Pass), []byte(hp.Pass)) == 1
	}
	return false
}

// NeedsRehash returns true when the password is not a bcrypt hash. Such a password should be replaced
// with NewPassword after it is verified with Valid, for example on login.
func (t Password) NeedsRehash() bool {
	return t.parts().Meth != "crypt"
}

// NewPassword creates a new bcrypt hash of the password. It returns bcrypt.ErrPasswordTooLong
// when the password is longer than 72 bytes.
func NewPassword(s string) (Password, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(s), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return Password("{crypt}" + string(hash)), nil
}

// HashPassword creates a password hash with the legacy salted SHA-1 method "ssha1", it is used to verify
// the passwords created with it. Other methods return an empty password, use NewPassword for new passwords.
func HashPassword(meth, pass, salt string) Password {
	switch meth {
	case "ssha1":
		h := sha1.New()
		io.WriteString(h, pass)
		io.WriteString(h, salt)
		res := append(h.Sum(nil), []byte(salt)...)
		return Password(base64.StdEncoding.EncodeToString(res))
	}
	return ""
}
