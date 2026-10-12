package logging

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
)

// Logger là wrapper slog của zcloud. Wrapper giữ các method tương thích
// Printf/Print/Println trong quá trình chuyển đổi dần sang structured attrs.
type Logger struct {
	*slog.Logger
}

func New(level int) *Logger {
	slogLevel := slog.LevelInfo
	if level >= 1 {
		slogLevel = slog.LevelDebug
	}
	return NewWithHandler(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slogLevel}))
}

func NewWithHandler(handler slog.Handler) *Logger {
	return &Logger{Logger: slog.New(handler)}
}

func Discard() *Logger {
	return NewWithHandler(slog.NewTextHandler(io.Discard, nil))
}

func (l *Logger) Printf(format string, args ...any) {
	l.Info(fmt.Sprintf(format, args...), "compat", true)
}

func (l *Logger) Print(args ...any) {
	l.Info(strings.TrimRight(fmt.Sprint(args...), "\n"), "compat", true)
}

func (l *Logger) Println(args ...any) {
	l.Info(strings.TrimRight(fmt.Sprintln(args...), "\n"), "compat", true)
}

func (l *Logger) Fatalf(format string, args ...any) {
	l.Error(fmt.Sprintf(format, args...), "fatal", true)
	os.Exit(1)
}
