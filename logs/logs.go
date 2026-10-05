// Package logs writes the application log messages with the standard logger, and reports errors to Sentry
// when it is initialized by InitSentry.
package logs

import (
	"fmt"
	"log"
)

// Errorf logs error msg with format
func Errorf(msg string, args ...any) {
	log.Println("[ERROR]", fmt.Sprintf(msg, args...))
}

// Error logs error msg
func Error(err error, msg string) {
	log.Println("[ERROR]", msg, err)
}

// Debug logs debug msg
func Debug(msg string) {
	log.Println("[DEBUG]", msg)
}

// Info logs info msg
func Info(msg string) {
	log.Println("[INFO]", msg)
}

// Infof logs info msg with format
func Infof(msg string, args ...any) {
	log.Println("[INFO]", fmt.Sprintf(msg, args...))
}

// Fatal logs fatal msg, sends the buffered Sentry events and terminates program
func Fatal(err error, msg string) {
	flushSentry()
	log.Fatalln("[FATAL]", msg, err)
}

// Fatalf logs fatal msg with format, sends the buffered Sentry events and terminates program
func Fatalf(msg string, args ...any) {
	flushSentry()
	log.Fatalf("[FATAL] "+msg, args...)
}
