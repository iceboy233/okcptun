package main

import (
	"flag"
	"io"
	"log"
	"net"
	"okcptun"
	"sync"

	"github.com/xtaci/kcp-go/v5"
)

var (
	flagLocalAddr  = flag.String("localAddr", "127.0.0.1:10000", "")
	flagTargetAddr = flag.String("targetAddr", "127.0.0.1:10000", "")
)

func main() {
	localAddr, err := net.ResolveUDPAddr("udp", *flagLocalAddr)
	if err != nil {
		log.Fatal("main: ResolveUDPAddr failed: ", err)
	}
	targetAddr, err := net.ResolveTCPAddr("tcp", *flagTargetAddr)
	if err != nil {
		log.Fatal("main: ResolveTCPAddr failed: ", err)
	}
	conn, err := net.ListenUDP("udp", localAddr)
	if err != nil {
		log.Fatal("main: ListenUDP failed: ", err)
	}
	log.Print("main: Listening on ", conn.LocalAddr())
	mux := okcptun.NewKCPMux(conn)
	for {
		clientConn, err := mux.Accept()
		if err != nil {
			log.Fatal("main: Accept failed: ", err)
		}
		go handle(clientConn, targetAddr)
	}
}

func handle(clientConn *kcp.UDPSession, targetAddr *net.TCPAddr) {
	log.Print("handle: new connection from ", clientConn.RemoteAddr())
	targetConn, err := net.DialTCP("tcp", nil, targetAddr)
	if err != nil {
		log.Print("handle: DialTCP failed: ", err)
		return
	}
	wg := &sync.WaitGroup{}
	wg.Add(2)
	go pipe(clientConn, targetConn, wg)
	go pipe(targetConn, clientConn, wg)
	wg.Wait()
}

func pipe(reader io.ReadCloser, writer io.WriteCloser, wg *sync.WaitGroup) {
	defer reader.Close()
	defer writer.Close()

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
