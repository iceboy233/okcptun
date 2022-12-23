package okcptun

import (
	"io"
)

func Pipe(reader io.Reader, writer io.Writer, done chan struct{}) {
	defer func() { done <- struct{}{} }()
	buffer := [8192]byte{}
	for {
		size, err := reader.Read(buffer[:])
		if err != nil {
			return
		}
		_, err = writer.Write(buffer[:size])
		if err != nil {
			return
		}
	}
}
