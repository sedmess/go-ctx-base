package logconfig

import (
	slogmulti "github.com/samber/slog-multi"
	"github.com/sedmess/go-ctx/ctx"
	"gopkg.in/natefinch/lumberjack.v2"
	"log/slog"
	"os"
	"strings"
)

//goland:noinspection GoUnusedConst
const INIT = ""

func init() {
	ctx.Env[loggingConfig]().configure(nil)
}

func InitWithExtraHandlers(handlers ...slog.Handler) {
	ctx.Env[loggingConfig]().configure(handlers)
}

type loggingConfig struct {
	level string `env:"LOG_LEVEL=info"`
	//lumberjack
	filePath       string `env:"LOG_FILE_PATH="`
	fileMaxSize    int    `env:"LOG_FILE_MAX_SIZE=10"`
	fileMaxBackups int    `env:"LOG_FILE_MAX_BACKUPS=3"`
	fileMaxAge     int    `env:"LOG_FILE_MAX_AGE=30"`
	fileCompress   bool   `env:"LOG_FILE_COMPRESS=true"`
}

func (c loggingConfig) configure(extraHandlers []slog.Handler) {
	var handlers []slog.Handler

	var level slog.Level
	switch strings.ToLower(c.level) {
	case "debug":
		level = slog.LevelDebug
	case "info":
		level = slog.LevelInfo
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	handlers = append(handlers, slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{AddSource: true, Level: level}))
	if c.filePath != "" {
		loggerFile := &lumberjack.Logger{
			Filename:   c.filePath,
			MaxSize:    c.fileMaxSize,
			MaxBackups: c.fileMaxBackups,
			MaxAge:     c.fileMaxAge,
			Compress:   c.fileCompress,
		}
		handlers = append(handlers, slog.NewTextHandler(loggerFile, &slog.HandlerOptions{AddSource: true, Level: level}))
	}
	if len(extraHandlers) > 0 {
		handlers = append(handlers, extraHandlers...)
	}
	ctx.SetSlogHandler(slogmulti.Fanout(handlers...))
}
