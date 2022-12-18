package main

import (
	"flag"
	"io"
	"log"
	"net"
	"okcptun"
	"sync"
)

var (
	flagLocalAddr  = flag.String("localAddr", "127.0.0.1:10000", "")
	flagRemoteAddr = flag.String("remoteAddr", "127.0.0.1:10000", "")
)

func main() {
	localAddr, err := net.ResolveTCPAddr("tcp", *flagLocalAddr)
	if err != nil {
		log.Fatal("main: ResolveTCPAddr failed: ", err)
	}
	remoteAddr, err := net.ResolveUDPAddr("udp", *flagRemoteAddr)
	if err != nil {
		log.Fatal("main: ResolveUDPAddr failed: ", err)
	}
	listener, err := net.ListenTCP("tcp", localAddr)
	if err != nil {
		log.Fatal("main: ListenTCP failed: ", err)
	}
	log.Print("main: Listening on ", listener.Addr())
	conn, err := net.ListenUDP("udp", nil)
	if err != nil {
		log.Fatal("main: ListenUDP failed: ", err)
	}
	mux := okcptun.NewKCPMux(conn)
	for {
		clientConn, err := listener.AcceptTCP()
		if err != nil {
			log.Fatal("main: AcceptTCP failed: ", err)
		}
		go handle(clientConn, mux, remoteAddr)
	}
}

func handle(clientConn net.Conn, mux *okcptun.KCPMux, remoteAddr *net.UDPAddr) {
	log.Print("handle: new connection from ", clientConn.RemoteAddr())
	remoteConn, err := mux.Dial(remoteAddr)
	if err != nil {
		log.Print("handle: Dial failed: ", err)
		return
	}
	wg := &sync.WaitGroup{}
	wg.Add(2)
	go pipe(clientConn, remoteConn, wg)
	go pipe(remoteConn, clientConn, wg)
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
