package validator

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEmailFormat(t *testing.T) {
	re := regexp.MustCompile(EmailFormat)
	for _, s := range []string{"john@example.com", "john.doe+tag@mail.example.co.uk", `"john doe"@example.com`} {
		assert.True(t, re.MatchString(s), s)
	}
	for _, s := range []string{"", "john", "john@", "@example.com", "john@localhost", "john..doe@example.com", "john@-example.com"} {
		assert.False(t, re.MatchString(s), s)
	}
}

func TestPhoneFormat(t *testing.T) {
	re := regexp.MustCompile(PhoneFormat)
	for _, s := range []string{"+1 (555) 123-4567", "+15551234567", "555.123.4567", "+44 20 7946 0958 ext. 12"} {
		assert.True(t, re.MatchString(s), s)
	}
	for _, s := range []string{"phone", "+1 555 abc"} {
		assert.False(t, re.MatchString(s), s)
	}
}
