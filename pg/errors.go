package pg

import (
	"fmt"
	"runtime"
	"strings"
)

var (
	// ErrRecordNotFound is returned when no record matches a find or a query.
	ErrRecordNotFound = Errorf("record not found")
	// ErrNoRowsAffected is returned by UpdateModel and DeleteModel when there is no record with the model ID.
	ErrNoRowsAffected = Errorf("no rows affected")
	// ErrDatabaseAlreadyConnected is returned by Connect when DB is already connected.
	ErrDatabaseAlreadyConnected = Errorf("database already connected")
	// ErrNotConnected is returned by the queries run before Connect or after Close.
	ErrNotConnected = Errorf("database not connected")
)

// Errorf is like fmt.Errorf with the "[PG] " prefix, the prefix of all errors returned by the package.
func Errorf(format string, args ...any) error {
	return fmt.Errorf("[PG] "+format, args...)
}

// maxRegistrationFrames is the most frames of the registration stack kept for a RegistrationError.
const maxRegistrationFrames = 8

// RegistrationError is returned by Connect when a registered repository setup or init function fails.
// Its message has the short stack of the RegisterRepository or RegisterInitFunc call, up to the runtime frames
// running the package init functions or the goroutine:
//
//	[PG] init function failed: relation "admins" does not exist
//	registered at:
//		myapp/models.init.0
//			/src/myapp/models/admin.go:15
type RegistrationError struct {
	// What is the failed registration: "init function" or "setup of repository *repo.Cached[...]".
	What string
	// Err is the error of the failed setup or init function.
	Err error
	pcs []uintptr
}

func (e *RegistrationError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "[PG] %s failed: %v\nregistered at:", e.What, e.Err)
	for _, f := range e.Stack() {
		fmt.Fprintf(&b, "\n\t%s\n\t\t%s:%d", f.Function, f.File, f.Line)
	}
	return b.String()
}

func (e *RegistrationError) Unwrap() error {
	return e.Err
}

// Stack returns the frames of the registration call, without the runtime frames.
func (e *RegistrationError) Stack() []runtime.Frame {
	var stack []runtime.Frame
	frames := runtime.CallersFrames(e.pcs)
	for {
		f, more := frames.Next()
		// runtime.doInit and runtime.main run the package init functions, runtime.goexit ends every goroutine
		if f.Function == "" || strings.HasPrefix(f.Function, "runtime.") {
			break
		}
		stack = append(stack, f)
		if !more {
			break
		}
	}
	return stack
}

// SQLSTATE codes of the constraint violations, the Code of a PgError.
const (
	NotNullViolation    = "23502"
	ForeignKeyViolation = "23503"
	UniqueViolation     = "23505"
	CheckViolation      = "23514"
	ExclusionViolation  = "23P01"
)

// KeyDetail returns the columns and the values of the key in the Detail of a unique, foreign key or exclusion
// violation: "ip" and "1.1.1.1" for "Key (ip)=(1.1.1.1) already exists.". The columns and values of a multicolumn
// key are separated by ", " as the server writes them. ok is false when the Detail has no key, the server leaves it
// out when the user cannot read the values.
func KeyDetail(err *PgError) (columns, values string, ok bool) {
	rest, found := strings.CutPrefix(err.Detail, "Key (")
	if !found {
		return "", "", false
	}
	columns, rest, found = strings.Cut(rest, ")=(")
	if !found {
		return "", "", false
	}
	// the values end before the message, the last ") " as the values may contain one too
	end := strings.LastIndex(rest, ") ")
	if end < 0 {
		return "", "", false
	}
	return columns, rest[:end], true
}
