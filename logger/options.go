package logger

// Options
type Options struct {
	LogPath string // def /log
	Level   *Level // CRIT,ERRO,WARN,NOTI,INFO,DEBU
}

type Option func(o *Options)

// setter
// SetLogPath
func SetLogPath(logPath string) Option {
	return func(o *Options) {
		o.LogPath = logPath
	}
}

// SetLevel
func SetLevel(level Level) Option {
	return func(o *Options) {
		o.Level = &level
	}
}
