package ds

import (
	"fmt"
	"github.com/smallnest/ringbuffer"
	"testing"
	"time"
)

func TestByteBlockChan_Push(t *testing.T) {
	b:=&BQ{Size: 10}
	b.Push([]byte("zhang")...)
	b.Push([]byte("zhang")...)
	go func() {
		time.Sleep(2*time.Second)
		bs:=make([]byte,5,5)
		n:=b.Pop(bs)
		fmt.Println(string(bs[:n]))
	}()
	b.Push([]byte("zhang")...)
	fmt.Println(b.Len())
	go func() {
		time.Sleep(2*time.Second)
		bs:=make([]byte,5,5)
		n:=b.Pop(bs)
		fmt.Println(string(bs[:n]))
	}()
	b.Push([]byte("zhang")...)
	fmt.Println(b.Len())

}
func TestByteBlockChan_Len(t *testing.T) {
	s:=time.Now().UnixNano()
	c:=make(chan bool,65535)
	for i:=0;i<65535;i++{
		c<-true
	}
	for i:=0;i<65535;i++{
		<-c
	}
	fmt.Println(time.Now().UnixNano()-s)
	s=time.Now().UnixNano()
	ss:=make([]bool,0)
	for i:=0;i<65535;i++{
		ss=append(ss,true)
	}
	n:=len(ss)
	sss:=make([]bool,0)
	copy(sss,ss[:n])
	ss=ss[n:]
	fmt.Println(time.Now().UnixNano()-s)
}

func TestByteBlockChan_PopWithStop(t *testing.T) {
	//bs:=make([]byte,5)
	//buf:=bytes.NewBuffer(make([]byte,5))
	//_,err:=buf.Read(bs)
	//fmt.Println(err,string(bs))
	//_,err=buf.Read(bs)
	//fmt.Println(err,string(bs))
	buf :=ringbuffer.New(5)
	bs:=make([]byte,5)
	_,err:=buf.Read(bs)
	fmt.Println(err,string(bs))
	//io.ReadFull()
}
func TestByteBlockChan_Pop(t *testing.T) {
	b:=&BQ{Size: 10}
	bs:=make([]byte,5,5)
	go func() {
		time.Sleep(2*time.Second)
		b.Push([]byte("zhang")...)
	}()
	n:=b.Pop(bs)
	fmt.Println(string(bs[:n]),b.Len())
	go func() {
		time.Sleep(2*time.Second)
		b.Push([]byte("zhang")...)
	}()
	n=b.Pop(bs)
	fmt.Println(string(bs[:n]),b.Len())

}