package logger

import (
	"fmt"
	"time"
)

// LogMessage is a message with its level and time, added to LogMessages.
type LogMessage struct {
	Level   LogLevel  `json:"level,omitempty"`
	Time    time.Time `json:"time,omitempty"`
	Message string    `json:"message,omitempty"`
}

// LogMessages collects leveled messages, encoded in JSON as "messages".
type LogMessages struct {
	Messages []LogMessage `json:"messages,omitempty"`
}

// AddMessage adds the message formatted with the args at the level and the current time.
// An empty message is not added.
func (l *LogMessages) AddMessage(level LogLevel, message string, args ...any) {
	msg := fmt.Sprintf(message, args...)
	if msg == "" {
		return
	}
	l.Messages = append(l.Messages, LogMessage{
		Level:   level,
		Time:    time.Now(),
		Message: msg,
	})
}

// AddInfo adds an INFO message, see AddMessage.
func (l *LogMessages) AddInfo(message string, args ...any) {
	l.AddMessage(INFO, message, args...)
}

// AddError adds an ERROR message, see AddMessage.
func (l *LogMessages) AddError(message string, args ...any) {
	l.AddMessage(ERROR, message, args...)
}

// AddDebug adds a DEBUG message, see AddMessage.
func (l *LogMessages) AddDebug(message string, args ...any) {
	l.AddMessage(DEBUG, message, args...)
}

// AddWarn adds a WARN message, see AddMessage.
func (l *LogMessages) AddWarn(message string, args ...any) {
	l.AddMessage(WARN, message, args...)
}
