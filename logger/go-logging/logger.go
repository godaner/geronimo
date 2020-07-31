package go_logging

import (
	"github.com/godaner/geronimo/logger"
	logging "github.com/op/go-logging"
	"os"
)

var levs = map[string]logger.Level{
	"CRIT": logger.CRITICAL,
	"ERRO": logger.ERROR,
	"WARN": logger.WARNING,
	"NOTI": logger.NOTICE,
	"INFO": logger.INFO,
	"DEBU": logger.DEBUG,
}

type Logger struct {
	logger *logging.Logger
	L      logger.Level
}

func (l *Logger) Noticef(fms string, arg ...interface{}) {
	l.logger.Noticef(fms, arg...)
}

func (l *Logger) Notice(arg ...interface{}) {
	l.logger.Notice(arg...)
}

func (l *Logger) Criticalf(fms string, arg ...interface{}) {
	l.logger.Criticalf(fms, arg...)
}

func (l *Logger) Critical(arg ...interface{}) {
	l.logger.Critical(arg...)
}

func (l *Logger) Debugf(fms string, arg ...interface{}) {
	l.logger.Debugf(fms, arg...)
}
func (l *Logger) Debug(arg ...interface{}) {
	l.logger.Debug(arg...)
}

func (l *Logger) Infof(fms string, arg ...interface{}) {
	l.logger.Infof(fms, arg...)
}

func (l *Logger) Info(arg ...interface{}) {
	l.logger.Info(arg...)
}

func (l *Logger) Warningf(fms string, arg ...interface{}) {
	l.logger.Warningf(fms, arg...)
}

func (l *Logger) Warning(arg ...interface{}) {
	l.logger.Warning(arg...)
}

func (l *Logger) Errorf(fms string, arg ...interface{}) {
	l.logger.Errorf(fms, arg...)
}

func (l *Logger) Error(arg ...interface{}) {
	l.logger.Error(arg...)
}

var (
	module  string
	logPath string
	level   *logger.Level
)

// SetLogger
func SetLogger(mod string, options ...logger.Option) {
	module = mod
	// options
	opts := &logger.Options{}
	for _, v := range options {
		if v == nil {
			continue
		}
		v(opts)
	}
	logPath = opts.LogPath
	level = opts.Level
	if level == nil {
		level = getEnvLev()
	}
	if logPath == "" {
		logPath = getEnvLogPath()
	}
}

// GetLogger
func GetLogger(tag string) logger.Logger {
	if module == "" {
		panic("pls call SetLogger first")
	}
	// logger
	logger, err := logging.GetLogger(module)
	if err != nil {
		panic(err)
	}
	if logger == nil {
		panic("nil logger")
	}
	if tag != "" {
		tag = " " + tag
	}
	var format = logging.MustStringFormatter(
		"%{color}%{time:2006-01-02 15:04:05.000} " + module + tag + " > %{level:.4s} %{color:reset} %{message}",
	)
	// std
	stdLog := logging.NewLogBackend(os.Stdout, "", 0)
	stdLogF := logging.NewBackendFormatter(stdLog, format)
	// file
	fileLog := logging.NewLogBackend(NewLogWrite(logPath+"/"+module, module), "", 0)
	fileLogF := logging.NewBackendFormatter(fileLog, format)
	// set lev
	le := logging.MultiLogger(stdLogF, fileLogF)
	le.SetLevel(logging.Level(*level), "")
	logger.SetBackend(le)
	return &Logger{logger: logger}
}

// getEnvLogPath
func getEnvLogPath() string {
	lp := os.Getenv("LOG_PATH")
	if lp != "" {
		return lp
	}
	return logger.DefLogPath
}

// getEnvLev
func getEnvLev() *logger.Level {
	ls := os.Getenv("LOG_LEV")
	l, ok := levs[ls]
	if !ok {
		l = logger.INFO
	}
	return &l
}
