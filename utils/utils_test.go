package utils

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRandom(t *testing.T) {
	assert.Regexp(t, `^[0-9]{8}$`, RandomNumber(8))
	assert.Regexp(t, `^[0-9a-zA-Z]{16}$`, RandomString(16))
	assert.Regexp(t, `^[0-9a-zA-Z#@$%^*]{16}$`, RandomPassword(16))
	assert.Empty(t, RandomString(0))
	assert.NotEqual(t, RandomString(16), RandomString(16))
}

func TestTypeCreate(t *testing.T) {
	type item struct{ Name string }
	assert.Equal(t, &item{}, TypeCreate[*item]())
	assert.Equal(t, item{}, TypeCreate[item]())
	assert.Nil(t, TypeCreate[any]())
}

func TestTypeFields(t *testing.T) {
	type inner struct{ B, C int }
	type outer struct {
		A int
		inner
		d int
	}
	assert.Equal(t, [][]int{{0}, {1, 0}, {1, 1}}, TypeFields(reflect.TypeFor[outer](), nil))
}

func TestSanitizeString(t *testing.T) {
	assert.Equal(t, "hello world", SanitizeString(" hello\x00 worldㅤ "))
}
