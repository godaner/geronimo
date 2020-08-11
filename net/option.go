package net

type Options struct {
	OverBose bool
}

type Option func(o *Options)

func SetOverBose(ob bool) Option {
	return func(o *Options) {
		o.OverBose = ob
	}
}
