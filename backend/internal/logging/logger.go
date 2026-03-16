package logging

import (
	"fmt"
	"log"
	"os"
	"strings"
)

type Level int

const (
	DebugLevel Level = iota
	InfoLevel
	WarnLevel
	ErrorLevel
)

func ParseLevel(raw string) (Level, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "info":
		return InfoLevel, nil
	case "debug":
		return DebugLevel, nil
	case "warn", "warning":
		return WarnLevel, nil
	case "error":
		return ErrorLevel, nil
	default:
		return InfoLevel, fmt.Errorf("invalid LOG_LEVEL: %s", raw)
	}
}

type Logger struct {
	std    *log.Logger
	level  Level
	prefix string
}

func New(levelRaw string) (*Logger, error) {
	level, err := ParseLevel(levelRaw)
	if err != nil {
		return nil, err
	}
	return &Logger{
		std:   log.New(os.Stdout, "", log.LstdFlags),
		level: level,
	}, nil
}

func MustNew(levelRaw string) *Logger {
	logger, err := New(levelRaw)
	if err != nil {
		panic(err)
	}
	return logger
}

func (l *Logger) WithPrefix(prefix string) *Logger {
	mergedPrefix := strings.TrimSpace(prefix)
	if strings.TrimSpace(l.prefix) != "" {
		mergedPrefix = strings.TrimSpace(strings.TrimSpace(l.prefix) + " " + mergedPrefix)
	}
	return &Logger{
		std:    l.std,
		level:  l.level,
		prefix: mergedPrefix,
	}
}

func (l *Logger) Debugf(format string, args ...interface{}) {
	l.logf(DebugLevel, "level=debug", format, args...)
}

func (l *Logger) Infof(format string, args ...interface{}) {
	l.logf(InfoLevel, "level=info", format, args...)
}

func (l *Logger) Warnf(format string, args ...interface{}) {
	l.logf(WarnLevel, "level=warn", format, args...)
}

func (l *Logger) Errorf(format string, args ...interface{}) {
	l.logf(ErrorLevel, "level=error", format, args...)
}

func (l *Logger) logf(level Level, levelLabel string, format string, args ...interface{}) {
	if l == nil || l.std == nil {
		return
	}
	if level < l.level {
		return
	}
	msg := fmt.Sprintf(format, args...)
	prefix := strings.TrimSpace(strings.TrimSpace(l.prefix) + " " + levelLabel)
	if prefix == "" {
		l.std.Print(msg)
		return
	}
	l.std.Printf("%s %s", prefix, msg)
}
