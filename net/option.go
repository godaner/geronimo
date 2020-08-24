package net

type Options struct {
	Enc      string // method@password
}

type Option func(o *Options)
// SetEnc
func SetEnc(enc string) Option {
	return func(o *Options) {
		o.Enc = enc
	}
}
