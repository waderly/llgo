// RUN: llgo -o %t %s
// RUN: %t > %t1 2>&1
// RUN: go run %s > %t2 2>&1
// RUN: diff -u %t1 %t2

package main

type namedByte byte

func testBytesConversion() {
	s := "abc"
	b := []byte(s)
	println("testBytesConversion:", s == string(b))
	nb := []namedByte(s)
	for _, v := range nb {
		println(v)
	}
	b[0] = '!'
	println(s)
	s = string(b)
	b[0] = 'a'
	println(s)
}

func testBytesCopy() {
	s := "abc"
	b := make([]byte, len(s))
	copy(b, s)
	println("testBytesCopy:", string(b) == s)
}

func main() {
	testBytesConversion()
	testBytesCopy()
}
