package validator

// Formats of common values, for Format, RequiredFormat and ValidateFormat.
const (
	// EmailFormat matches an email address: a dot-atom or quoted local part, then a domain with at least two labels.
	EmailFormat = `^([-!#-\'*+\/-9=?A-Z^-~]{1,64}(\.[-!#-\'*+\/-9=?A-Z^-~]{1,64})*|"([]!#-[^-~ \t]|(\\[\t -~]))+")@[0-9A-Za-z]([0-9A-Za-z-]{0,61}[0-9A-Za-z])?(\.[0-9A-Za-z]([0-9A-Za-z-]{0,61}[0-9A-Za-z])?)+$`

	// PhoneFormat matches a phone number, with an optional country code and extension, and digits separated by
	// spaces, dots, dashes, slashes or parentheses.
	PhoneFormat = `^(?:(?:\(?(?:00|\+)([1-4]\d\d|[1-9]\d?)\)?)?[\-\.\ \\\/]?)?((?:\(?\d{1,}\)?[\-\.\ \\\/]?){0,})(?:[\-\.\ \\\/]?(?:#|ext\.?|extension|x)[\-\.\ \\\/]?(\d+))?$`
)
