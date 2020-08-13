package cipher

import (
	"strings"
	"testing"
)

func TestCipherr_Encrypt(t *testing.T) {
	s := strings.Repeat("zhangke", 100)
	c1, err := NewCipher("aes-256-cfb", "123qweccccccc")
	if err != nil {
		panic(err)
	}
	c2, err := NewCipher("aes-256-cfb", "123qweccccccc")
	if err != nil {
		panic(err)
	}
	//ss := make([]byte, len(s), len(s))
	ss:=c1.Encrypt([]byte(s))
	if len(ss) == len(s) {
		t.Fatal(false)
	}
	t.Log(string(s))
	t.Log(string(ss))
	if string(ss) == s {
		t.Fatal(false)
	}
	//sss := make([]byte, len(s), len(s))
	sss:=c2.Decrypt( ss)
	if string(sss) != s {
		t.Fatal(false)
	}
}
