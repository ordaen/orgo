package validator

import (
	"errors"
	"regexp"
	"regexp/syntax"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type validValue bool

func (v validValue) Valid() bool { return bool(v) }

func TestVerify(t *testing.T) {
	v := &Validator{}
	assert.NoError(t, v.Verify())

	v.Present("", "name")
	v.Present("john", "login")
	v.AddError("custom %d", 1)
	err := v.Verify()
	assert.EqualError(t, err, "name required, custom 1")
	var verr *Error
	require.True(t, errors.As(err, &verr))
	assert.Equal(t, []string{"name required", "custom 1"}, verr.Errors)

	v.Present("", "other")
	assert.Len(t, verr.Errors, 2, "the returned errors are a copy")
}

// TestPercentInMessages checks the messages and the attribute names are not format strings.
func TestPercentInMessages(t *testing.T) {
	v := &Validator{}
	v.PresentVariant("x", "discount", []string{"10%", "50%"})
	v.AttributeError("rate%", "too high")
	v.AddError("100% wrong")
	assert.EqualError(t, v.Verify(), "discount invalid. valid values: 10%, 50%, rate% too high, 100% wrong")

	v = &Validator{}
	v.AttributeError("name", "is taken by user %d", 5)
	assert.EqualError(t, v.Verify(), "name is taken by user 5")
}

func TestValid(t *testing.T) {
	v := &Validator{}
	assert.True(t, v.Valid(validValue(true), "id"))
	assert.False(t, v.Valid(validValue(false), "id"))
	assert.EqualError(t, v.Verify(), "id invalid")
}

func TestPresentVariant(t *testing.T) {
	v := &Validator{}
	assert.True(t, v.PresentVariant("b", "kind", []string{"a", "b"}))
	assert.False(t, v.PresentVariant("c", "kind", []string{"a", "b"}))
	assert.EqualError(t, v.Verify(), "kind invalid. valid values: a, b")
}

func TestStringLength(t *testing.T) {
	v := &Validator{}
	assert.True(t, v.StringLength("пароль", "password", 6, 6), "characters, not bytes")
	assert.True(t, v.StringLength("", "password", 0, 0), "bounds <= 0 are not checked")
	assert.False(t, v.StringLength("abc", "password", 6, 18))
	assert.False(t, v.StringLength("abcdefgh", "password", 2, 4))
	assert.EqualError(t, v.Verify(), "password min length is 6, password max length is 4")
}

func TestNumbers(t *testing.T) {
	v := &Validator{}
	assert.True(t, v.Int(5, "n", 5, 10), "the bounds are included")
	assert.True(t, v.Int(10, "n", 5, 10))
	assert.True(t, v.Int(-50, "n", 0, 10), "a bound <= 0 is not checked")
	assert.False(t, v.Int(4, "n", 5, 10))
	assert.False(t, v.Int64(11, "n64", 5, 10))
	assert.False(t, v.Float32(0.5, "f32", 1, 2))
	assert.False(t, v.Float64(2.5, "f64", 1, 2))
	assert.EqualError(t, v.Verify(), "n should be >= 5, n64 should be <= 10, f32 should be >= 1, f64 should be <= 2")

	v = &Validator{}
	assert.True(t, v.Int64Present(1, "id"))
	assert.False(t, v.Int64Present(0, "id"))
	assert.EqualError(t, v.Verify(), "id required")
}

func TestBetween(t *testing.T) {
	v := &Validator{}
	assert.True(t, Between(v, -40, "temperature", -40, 60))
	assert.True(t, Between(v, 0, "temperature", -40, 60))
	assert.False(t, Between(v, -41, "temperature", -40, 60))
	assert.False(t, Between(v, 1, "count", 0, 0), "bounds <= 0 are checked")
	assert.True(t, Between(v, "m", "letter", "a", "z"))
	assert.False(t, Between(v, 0.5, "ratio", -1.0, 0.25))
	assert.EqualError(t, v.Verify(), "temperature should be >= -40, count should be <= 0, ratio should be <= 0.25")
}

func TestFormat(t *testing.T) {
	const ip = `\A(\d{1,3}\.){3}\d{1,3}\z`
	v := &Validator{}
	assert.True(t, v.Format("", "ip", ip), "an empty string is valid")
	assert.True(t, v.Format("1.2.3.4", "ip", ip))
	assert.False(t, v.Format("1.2.3", "ip", ip))
	assert.False(t, v.RequiredFormat("", "ip", ip))
	assert.EqualError(t, v.Verify(), "ip invalid format, ip invalid format")

	assert.NoError(t, ValidateFormat("1.2.3.4", "ip", ip))
	assert.EqualError(t, ValidateFormat("x", "ip", ip), "ip invalid format")
}

func TestFormatInvalidRegexp(t *testing.T) {
	v := &Validator{}
	assert.True(t, v.Format("", "f", `(`), "an empty string is not checked")
	assert.False(t, v.Format("x", "f", `(`))
	assert.False(t, v.RequiredFormat("x", "g", `(`), "the cached error is returned again")
	assert.EqualError(t, v.Verify(), "f invalid format: error parsing regexp: missing closing ): `(`, "+
		"g invalid format: error parsing regexp: missing closing ): `(`")

	err := ValidateFormat("x", "f", `[`)
	assert.EqualError(t, err, "f invalid format: error parsing regexp: missing closing ]: `[`")
	var syntaxErr *syntax.Error
	assert.ErrorAs(t, err, &syntaxErr)
}

func TestCompileCache(t *testing.T) {
	var wg sync.WaitGroup
	got := make([]*regexp.Regexp, 8)
	for i := range got {
		wg.Go(func() {
			r, err := compile(`\d+`)
			assert.NoError(t, err)
			got[i] = r
		})
	}
	wg.Wait()
	for _, r := range got {
		assert.Same(t, got[0], r, "the regular expression is compiled once")
	}
}

func BenchmarkFormat(b *testing.B) {
	v := &Validator{}
	for b.Loop() {
		v.Format("1.2.3.4", "ip", `\A(\d{1,3}\.){3}\d{1,3}\z`)
	}
}
