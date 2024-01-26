package logconfig

import (
	"github.com/sedmess/go-ctx/ctx"
	"github.com/sedmess/go-ctx/logger"
	"gopkg.in/natefinch/lumberjack.v2"
)

const logFilePathKey = "LOG_FILE_PATH"
const logMaxSizeKey = "LOG_FILE_MAX_SIZE"
const logMaxBackupsKey = "LOG_FILE_MAX_BACKUPS"
const logMaxAgeKey = "LOG_FILE_MAX_AGE"
const logCompressKey = "LOG_FILE_COMPRESS"

func init() {
	logFilePathVar := ctx.GetEnv(logFilePathKey)
	if logFilePathVar.IsPresent() {
		loggerFile := lumberjack.Logger{
			Filename:   logFilePathVar.AsString(),
			MaxSize:    ctx.GetEnv(logMaxSizeKey).AsIntDefault(10),
			MaxBackups: ctx.GetEnv(logMaxBackupsKey).AsIntDefault(3),
			MaxAge:     ctx.GetEnv(logMaxAgeKey).AsIntDefault(30),
			Compress:   ctx.GetEnv(logCompressKey).AsBoolDefault(true),
		}
		logger.SetWriter(&loggerFile)
	}
}

const INIT = ""
