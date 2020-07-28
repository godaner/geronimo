package v1

import (
	"fmt"
	"testing"
	"time"
)

func TestDial2(t *testing.T) {
	go func() {
		time.Sleep(2 * time.Second)
		l, err := Listen(&GAddr{
			IP:   "192.168.6.6",
			Port: 3333,
		})
		if err != nil {
			panic(err)
		}
		c2, err := l.Accept()
		if err != nil {
			panic(err)
		}
		fmt.Println(c2)
	}()
	c1, err := Dial(&GAddr{
		IP:   "192.168.6.6",
		Port: 3333,
	})
	fmt.Println(c1, err)

	//time.Sleep(100*time.Second)
}
