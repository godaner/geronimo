package new

import (
	"github.com/godaner/geronimo/rule"
	v1 "github.com/godaner/geronimo/rule/v1"
)

// NewMessage
func NewMessage(opts ...Option) (m rule.Message) {
	options := Options{}
	for _, o := range opts {
		o(&options)
	}
	return &v1.Message{}
}
