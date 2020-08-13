package fac

import (
	"github.com/godaner/geronimo/cipher"
	"github.com/godaner/geronimo/rule"
	v1 "github.com/godaner/geronimo/rule/v1"
	v2 "github.com/godaner/geronimo/rule/v2"
	"strings"
	"sync"
)

type Fac struct {
	sync.Once
	Enc string
	new func() (m rule.Message)
}

func (e *Fac) init() {
	e.Do(func() {
		if e.Enc == "" {
			e.new = func() (m rule.Message) {
				return &v1.Message{}
			}
			return
		}
		// method@password
		encp := strings.Split(e.Enc, "@")
		if len(encp) <= 1 {
			panic("enc is not right , " + e.Enc)
		}
		c, err := cipher.NewCipher(encp[0], encp[1])
		if err != nil {
			panic(err)
		}
		e.new = func() (m rule.Message) {
			return &v2.Message{
				C: c,
			}
		}
	})
}
func (e *Fac) New() (m rule.Message) {
	e.init()
	return e.new()
}
