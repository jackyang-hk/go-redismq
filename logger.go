package go_redismq

import (
	"context"
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

const (
	colorReset  = "\033[0m"
	colorRed    = "\033[31m"
	colorYellow = "\033[33m"
	colorBlue   = "\033[34m"
	colorGray   = "\033[90m"
)

type Logger interface {
	Debugf(format string, args ...any)
	Infof(format string, args ...any)
	Warnf(format string, args ...any)
	Errorf(format string, args ...any)
}

type ColorHandler struct {
	handler    slog.Handler
	w          io.Writer
	opts       *slog.HandlerOptions
	goidCache  map[uint64]string
	cacheMutex sync.RWMutex
}

func NewColorHandler(w io.Writer, opts *slog.HandlerOptions) *ColorHandler {
	if opts == nil {
		opts = &slog.HandlerOptions{}
	}

	return &ColorHandler{
		handler:   slog.NewTextHandler(w, opts),
		w:         w,
		opts:      opts,
		goidCache: make(map[uint64]string),
	}
}

func (h *ColorHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.handler.Enabled(ctx, level)
}

func (h *ColorHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &ColorHandler{
		handler:   h.handler.WithAttrs(attrs),
		w:         h.w,
		opts:      h.opts,
		goidCache: h.goidCache,
	}
}

func (h *ColorHandler) WithGroup(name string) slog.Handler {
	return &ColorHandler{
		handler:   h.handler.WithGroup(name),
		w:         h.w,
		opts:      h.opts,
		goidCache: h.goidCache,
	}
}

func (h *ColorHandler) Handle(ctx context.Context, r slog.Record) error {
	level := r.Level.String()
	color := getColorForLevel(r.Level)
	goid := getGoroutineID()

	var caller string

	showCaller := r.Level == slog.LevelDebug || r.Level == slog.LevelError

	if showCaller && h.opts.AddSource && r.PC != 0 {
		fs := runtime.CallersFrames([]uintptr{r.PC})
		f, _ := fs.Next()
		funcName := filepath.Base(f.Function)
		fileName := filepath.Base(f.File)
		caller = fmt.Sprintf("%s %s:%d", funcName, fileName, f.Line)
	}

	if caller != "" {
		fmt.Fprintf(h.w, "%s[%s]%s %s [thread-%d] [%s] %s",
			color,
			level,
			colorReset,
			r.Time.Format("2006-01-02 15:04:05"),
			goid,
			caller,
			r.Message,
		)
	} else {
		fmt.Fprintf(h.w, "%s[%s]%s %s [thread-%d] %s",
			color,
			level,
			colorReset,
			r.Time.Format("2006-01-02 15:04:05"),
			goid,
			r.Message,
		)
	}

	r.Attrs(func(a slog.Attr) bool {
		if a.Key != slog.SourceKey {
			fmt.Fprintf(h.w, " %s=%v", a.Key, a.Value)
		}

		return true
	})

	fmt.Fprintln(h.w)

	return nil
}

func getColorForLevel(level slog.Level) string {
	switch level {
	case slog.LevelDebug:
		return colorGray
	case slog.LevelInfo:
		return colorBlue
	case slog.LevelWarn:
		return colorYellow
	case slog.LevelError:
		return colorRed
	default:
		return colorReset
	}
}

func getGoroutineID() uint64 {
	b := make([]byte, 64)
	b = b[:runtime.Stack(b, false)]

	var goid uint64
	fmt.Sscanf(string(b), "goroutine %d ", &goid)

	return goid
}

func getLogLevelFromEnv() slog.Level {
	levelStr := strings.ToUpper(os.Getenv("LOG_LEVEL"))
	switch levelStr {
	case "DEBUG":
		return slog.LevelDebug
	case "INFO":
		return slog.LevelInfo
	case "WARN", "WARNING":
		return slog.LevelWarn
	case "ERROR":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

type slogLogger struct {
	logger *slog.Logger
}

func (l *slogLogger) Debugf(format string, args ...any) {
	l.logger.Debug(fmt.Sprintf(format, args...))
}

func (l *slogLogger) Infof(format string, args ...any) {
	l.logger.Info(fmt.Sprintf(format, args...))
}

func (l *slogLogger) Warnf(format string, args ...any) {
	l.logger.Warn(fmt.Sprintf(format, args...))
}

func (l *slogLogger) Errorf(format string, args ...any) {
	l.logger.Error(fmt.Sprintf(format, args...))
}

type stdLogger struct {
	logger *log.Logger
}

func (l *stdLogger) Debugf(format string, args ...any) {
	l.logger.Printf("[DEBUG] "+format, args...)
}

func (l *stdLogger) Infof(format string, args ...any) {
	l.logger.Printf("[INFO] "+format, args...)
}

func (l *stdLogger) Warnf(format string, args ...any) {
	l.logger.Printf("[WARN] "+format, args...)
}

func (l *stdLogger) Errorf(format string, args ...any) {
	l.logger.Printf("[ERROR] "+format, args...)
}

var logger Logger

func init() {
	level := getLogLevelFromEnv()
	slogInstance := slog.New(NewColorHandler(os.Stdout, &slog.HandlerOptions{
		Level:     level,
		AddSource: true,
	}))
	logger = &slogLogger{logger: slogInstance}
}

func SetLogger(l Logger) {
	if l != nil {
		logger = l
	}
}

func SetStdLogger(l *log.Logger) {
	if l != nil {
		logger = &stdLogger{logger: l}
	}
}

func SetSlogLogger(l *slog.Logger) {
	if l != nil {
		logger = &slogLogger{logger: l}
	}
}

func GetLogger() Logger {
	return logger
}
