package go_logging

import (
	"github.com/godaner/geronimo/logger"
	logging "github.com/op/go-logging"
	"os"
)

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
func NewLogger(module string, l logger.Level) logger.Logger {
	logger, err := logging.GetLogger(module)
	if err != nil {
		panic(err)
	}
	if logger==nil{
		panic("nil logger")
	}
	var format = logging.MustStringFormatter(
		"%{color}%{time:2006-01-02 15:04:05.000} " + module + " > %{level:.4s} %{color:reset} %{message}",
	)
	backend := logging.NewLogBackend(os.Stdout, "", 0)
	backendFormatter := logging.NewBackendFormatter(backend, format)
	lvlBackend := logging.AddModuleLevel(backendFormatter)
	lvlBackend.SetLevel(logging.Level(l), "")
	logger.SetBackend(lvlBackend)
	return &Logger{logger: logger}
}
