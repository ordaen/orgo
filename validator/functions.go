package validator

import "fmt"

// ValidateFormat returns an error when the value does not match the regular expression,
// or the error of an invalid regular expression.
func ValidateFormat(value, field, regex string) error {
	r, err := compile(regex)
	if err != nil {
		return fmt.Errorf("%s invalid format: %w", field, err)
	}
	if !r.MatchString(value) {
		return fmt.Errorf("%s invalid format", field)
	}
	return nil
}
