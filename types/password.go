package types

import (
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"io"
	"regexp"

	"github.com/ordaen/orgo/utils"
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
		err := bcrypt.CompareHashAndPassword([]byte(ps.Pass), []byte(s))
		if err == nil {
			return true
		}
	}
	hp := HashPassword(ps.Meth, s, ps.salt()).parts()
	return subtle.ConstantTimeCompare([]byte(ps.Pass), []byte(hp.Pass)) == 1
}

// NewPassword creates new sha1 hash
func NewPassword(s string) Password {
	return HashPassword("ssha1", s, utils.RandomString(4))
}

// HashPassword creates password hash
func HashPassword(meth, pass, salt string) Password {
	var pwd string
	switch meth {
	case "ssha1":
		h := sha1.New()
		io.WriteString(h, pass)
		io.WriteString(h, salt)
		res := append(h.Sum(nil), []byte(salt)...)
		pwd = base64.StdEncoding.EncodeToString(res)
		return Password(pwd)
	}
	return Password(fmt.Sprintf("{%s}%s", meth, pwd))
}
