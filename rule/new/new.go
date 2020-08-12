package new

import (
	"github.com/godaner/geronimo/rule"
	v1 "github.com/godaner/geronimo/rule/v1"
)

// NewMessage
func NewMessage() (m rule.Message) {
	return &v1.Message{}
}

// SetOptions
func SetOptions(opts ...Option) {
	options := Options{}
	for _, o := range opts {
		o(&options)
	}
}
