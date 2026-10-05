// Package validator collects the validation errors of values, returned together by Verify.
//
//	v := &validator.Validator{}
//	v.Present(user.Name, "name")
//	v.StringLength(user.Password, "password", 6, 18)
//	v.Format(user.IP, "ip address", `\A(\d{1,3}\.){3}\d{1,3}\z`)
//	return v.Verify()
package validator

import (
	"cmp"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"sync"
	"unicode/utf8"
)

type ValidType interface {
	Valid() bool
}

// Error is returned by Verify with the validation errors.
type Error struct {
	Errors []string
}

func (e *Error) Error() string {
	return strings.Join(e.Errors, ", ")
}

// Validator collects the validation errors
type Validator struct {
	Errors []string
}

// AddError adds the error, formatted with the args when they are given
func (v *Validator) AddError(err string, args ...any) {
	if len(args) > 0 {
		err = fmt.Sprintf(err, args...)
	}
	v.Errors = append(v.Errors, err)
}

// AttributeError adds the error of the attribute, the message is formatted with the args when they are given
//
//	v.AttributeError("name", "is taken by user %d", id) // "name is taken by user 5"
func (v *Validator) AttributeError(pointer, msg string, args ...any) {
	if len(args) > 0 {
		msg = fmt.Sprintf(msg, args...)
	}
	v.Errors = append(v.Errors, pointer+" "+msg)
}

// Verify returns an *Error with the errors, or nil when there are none
func (v Validator) Verify() error {
	if len(v.Errors) > 0 {
		return &Error{Errors: slices.Clone(v.Errors)}
	}
	return nil
}

// Valid validates interface
//
//	v.Valid(ValidType, "name")
func (v *Validator) Valid(value ValidType, pointer string) bool {
	if !value.Valid() {
		v.AttributeError(pointer, "invalid")
		return false
	}
	return true
}

// Present validates string for presence
//
//	v.Present(SomeVariable, "name")
func (v *Validator) Present(value, pointer string) bool {
	if value == "" {
		v.AttributeError(pointer, "required")
		return false
	}
	return true
}

// PresentVariant validates the string is one of the variants
//
//	v.PresentVariant(SomeVariable, "name", []string{"variant1", "variant2"})
func (v *Validator) PresentVariant(value, pointer string, variants []string) bool {
	if !slices.Contains(variants, value) {
		v.AttributeError(pointer, "invalid. valid values: "+strings.Join(variants, ", "))
		return false
	}
	return true
}

// StringLength validates the length of the string in characters is between min and max,
// a bound <= 0 is not checked
//
//	v.StringLength(SomeVariable, "password", 6, 18) // min 6, max 18
func (v *Validator) StringLength(value, pointer string, min, max int) bool {
	n := utf8.RuneCountInString(value)
	valid := true
	if min > 0 && n < min {
		v.AttributeError(pointer, "min length is %d", min)
		valid = false
	}
	if max > 0 && n > max {
		v.AttributeError(pointer, "max length is %d", max)
		valid = false
	}
	return valid
}

// Int validates the value is between min and max, a bound <= 0 is not checked, see Between for any bounds
//
//	v.Int(IntValue, "number", 0, 11) // max 11
func (v *Validator) Int(value int, pointer string, min, max int) bool {
	return validateNumber(v, value, pointer, min, max)
}

// Int64Present validates if value is not 0
//
//	v.Int64Present(Int64Value, "number")
func (v *Validator) Int64Present(value int64, pointer string) bool {
	if value == 0 {
		v.AttributeError(pointer, "required")
		return false
	}
	return true
}

// Int64 validates the value is between min and max, a bound <= 0 is not checked, see Between for any bounds
func (v *Validator) Int64(value int64, pointer string, min, max int64) bool {
	return validateNumber(v, value, pointer, min, max)
}

// Float32 validates the value is between min and max, a bound <= 0 is not checked, see Between for any bounds
func (v *Validator) Float32(value float32, pointer string, min, max float32) bool {
	return validateNumber(v, value, pointer, min, max)
}

// Float64 validates the value is between min and max, a bound <= 0 is not checked, see Between for any bounds
func (v *Validator) Float64(value float64, pointer string, min, max float64) bool {
	return validateNumber(v, value, pointer, min, max)
}

// Between validates the value is between min and max, both included and checked, also when they are <= 0
//
//	validator.Between(v, temperature, "temperature", -40, 60)
func Between[T cmp.Ordered](v *Validator, value T, pointer string, min, max T) bool {
	return checkRange(v, value, pointer, min, max, true, true)
}

// Format validates the format of the string by the regular expression, an empty string is valid
//
//	v.Format(StringValue, "ip address", `\A(\d{1,3}\.){3}\d{1,3}\z`)
func (v *Validator) Format(value, pointer, reg string) bool {
	if value != "" {
		return v.RequiredFormat(value, pointer, reg)
	}
	return true
}

// RequiredFormat validates the format of the string by the regular expression.
// An invalid regular expression adds its error, the value is not valid then.
//
//	v.RequiredFormat(StringValue, "ip address", `\A(\d{1,3}\.){3}\d{1,3}\z`)
func (v *Validator) RequiredFormat(value, pointer, reg string) bool {
	r, err := compile(reg)
	if err != nil {
		v.AttributeError(pointer, "invalid format: %v", err)
		return false
	}
	if !r.MatchString(value) {
		v.AttributeError(pointer, "invalid format")
		return false
	}
	return true
}

func validateNumber[T cmp.Ordered](v *Validator, value T, pointer string, min, max T) bool {
	var zero T
	return checkRange(v, value, pointer, min, max, min > zero, max > zero)
}

// checkRange validates the value is between min and max, the bounds are checked when their flags are set.
func checkRange[T cmp.Ordered](v *Validator, value T, pointer string, min, max T, checkMin, checkMax bool) bool {
	var errs []string
	if checkMin && value < min {
		errs = append(errs, fmt.Sprintf("should be >= %v", min))
	}
	if checkMax && value > max {
		errs = append(errs, fmt.Sprintf("should be <= %v", max))
	}
	if len(errs) > 0 {
		v.AttributeError(pointer, strings.Join(errs, ", "))
		return false
	}
	return true
}

// regexps caches the compiled regular expressions and their errors by their source, they are usually constants.
var regexps sync.Map

// compiled is a compiled regular expression, or its compile error.
type compiled struct {
	re  *regexp.Regexp
	err error
}

// compile returns the compiled regular expression, or the error of an invalid one.
func compile(reg string) (*regexp.Regexp, error) {
	c, ok := regexps.Load(reg)
	if !ok {
		re, err := regexp.Compile(reg)
		c, _ = regexps.LoadOrStore(reg, compiled{re: re, err: err})
	}
	return c.(compiled).re, c.(compiled).err
}
