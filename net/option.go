package net

type Options struct {
	OverBose bool
	Enc      string // method@password
}

type Option func(o *Options)
// SetOverBose
func SetOverBose(ob bool) Option {
	return func(o *Options) {
		o.OverBose = ob
	}
}
// SetEnc
func SetEnc(enc string) Option {
	return func(o *Options) {
		o.Enc = enc
	}
}
