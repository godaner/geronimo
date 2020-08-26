package win

import (
	"fmt"
	"testing"
	"time"
)

func TestBtlBw_Com(t *testing.T) {
	b:=btlBw{
		CS:make(chan struct{}),
	}
	go func() {
		i:=0
		for ; ;  {
			b.Com()
			i++
			if i%2==0{
				<-time.After(100*time.Millisecond)
			}else{
				<-time.After(500*time.Millisecond)
			}
		}
	}()
	for ; ;  {
		<-time.After(1*time.Second)
		fmt.Println(b.Get())
	}
}
