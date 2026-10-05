package utils

import (
	"strings"
	"unicode"
)

// SanitizeString sanitizes a string by removing control characters and other unwanted characters
func SanitizeString(s string) string {
	s = strings.Map(func(r rune) rune {
		if r == '\u3164' || unicode.IsSymbol(r) || unicode.IsMark(r) || unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
	return strings.TrimSpace(s)
}
