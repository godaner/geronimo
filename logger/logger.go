package logger

type Level int

const (
	CRITICAL Level = iota
	ERROR
	WARNING
	NOTICE
	INFO
	DEBUG
)


type Logger interface {
	Debugf(fms string, arg ...interface{})
	Debug(arg ...interface{})
	Infof(fms string, arg ...interface{})
	Info(arg ...interface{})
	Noticef(fms string, arg ...interface{})
	Notice(arg ...interface{})
	Warningf(fms string, arg ...interface{})
	Warning(arg ...interface{})
	Errorf(fms string, arg ...interface{})
	Error(arg ...interface{})
	Criticalf(fms string, arg ...interface{})
	Critical(arg ...interface{})
}
