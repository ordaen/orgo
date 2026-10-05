package logger

import (
	"fmt"
	"time"
)

type LogMessage struct {
	Level   LogLevel  `json:"level,omitempty"`
	Time    time.Time `json:"time,omitempty"`
	Message string    `json:"message,omitempty"`
}

type LogMessages struct {
	Messages []LogMessage `json:"messages,omitempty"`
}

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

func (l *LogMessages) AddInfo(message string, args ...any) {
	l.AddMessage(INFO, message, args...)
}

func (l *LogMessages) AddError(message string, args ...any) {
	l.AddMessage(ERROR, message, args...)
}

func (l *LogMessages) AddDebug(message string, args ...any) {
	l.AddMessage(DEBUG, message, args...)
}

func (l *LogMessages) AddWarn(message string, args ...any) {
	l.AddMessage(WARN, message, args...)
}
