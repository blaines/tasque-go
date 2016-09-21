package main

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"os"
)

func check(e error) {
	if e != nil {
		panic(e)
	}
}

func main() {
	f, err := os.Create("golang.txt")
	check(err)
	defer f.Close()
	w := bufio.NewWriter(f)

	nBytes, nChunks := int64(0), int64(0)
	r := bufio.NewReader(os.Stdin)
	buf := make([]byte, 0, 4*1024)
	for {
		n, err := r.Read(buf[:cap(buf)])
		buf = buf[:n]
		if n == 0 {
			if err == nil {
				continue
			}
			if err == io.EOF {
				break
			}
			log.Fatal(err)
		}
		nChunks++
		nBytes += int64(len(buf))
		n4, err := w.WriteString(string(buf[:len(buf)]))
		log.Print(string(buf[:len(buf)]))
		fmt.Printf("wrote %d bytes\n", n4)
		if err != nil && err != io.EOF {
			log.Fatal(err)
		}
	}
	w.Flush()
	log.Println("Bytes:", nBytes, "Chunks:", nChunks)
}
