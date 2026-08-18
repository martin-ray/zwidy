package logging

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"zwidy/internal/config"
)

type Logger struct {
	format string
	level  int
	out    io.Writer
	mu     sync.Mutex
}

const (
	levelDebug = iota
	levelInfo
	levelWarn
	levelError
)

func New(cfg config.LoggingConfig) (*Logger, io.Closer, error) {
	var out io.Writer
	var closer io.Closer = nopCloser{}
	switch cfg.Output {
	case "", "stdout":
		out = os.Stdout
	case "stderr":
		out = os.Stderr
	default:
		f, err := os.OpenFile(cfg.Output, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return nil, nil, err
		}
		out = f
		closer = f
	}
	return &Logger{format: cfg.Format, level: parseLevel(cfg.Level), out: out}, closer, nil
}

func parseLevel(level string) int {
	switch level {
	case "debug":
		return levelDebug
	case "warn":
		return levelWarn
	case "error":
		return levelError
	default:
		return levelInfo
	}
	}

func (l *Logger) Debug(msg string, fields map[string]any) { l.log(levelDebug, "debug", msg, fields) }
func (l *Logger) Info(msg string, fields map[string]any)  { l.log(levelInfo, "info", msg, fields) }
func (l *Logger) Warn(msg string, fields map[string]any)  { l.log(levelWarn, "warn", msg, fields) }
func (l *Logger) Error(msg string, fields map[string]any) { l.log(levelError, "error", msg, fields) }

func (l *Logger) log(level int, levelName, msg string, fields map[string]any) {
	if level < l.level {
		return
	}
	entry := map[string]any{"timestamp": time.Now().UTC().Format(time.RFC3339Nano), "level": levelName, "message": msg}
	for k, v := range fields {
		entry[k] = v
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.format == "text" {
		fmt.Fprintf(l.out, "%s level=%s msg=%q", entry["timestamp"], levelName, msg)
		for k, v := range fields {
			fmt.Fprintf(l.out, " %s=%v", k, v)
		}
		fmt.Fprintln(l.out)
		return
	}
	_ = json.NewEncoder(l.out).Encode(entry)
	}

type nopCloser struct{}

func (nopCloser) Close() error { return nil }
