package main

import (
	"flag"
	"log"
	"net"
	"okcptun"
)

var (
	flagLocalAddr  = flag.String("localAddr", "127.0.0.1:10000", "")
	flagRemoteAddr = flag.String("remoteAddr", "127.0.0.1:10000", "")
	flagPassword   = flag.String("password", "", "")
)

func main() {
	flag.Parse()

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
	mux, err := okcptun.NewKCPMux(conn, *flagPassword)
	if err != nil {
		log.Fatal("main: NewCipher failed: ", err)
	}
	for {
		clientConn, err := listener.AcceptTCP()
		if err != nil {
			log.Fatal("main: AcceptTCP failed: ", err)
		}
		go handle(clientConn, mux, remoteAddr)
	}
}

func handle(
	clientConn *net.TCPConn, mux *okcptun.KCPMux, remoteAddr *net.UDPAddr) {
	defer clientConn.Close()

	log.Print("handle: new connection from ", clientConn.RemoteAddr())
	remoteConn, closer, err := mux.Dial(remoteAddr)
	if err != nil {
		log.Print("handle: Dial failed: ", err)
		return
	}
	defer closer.Close()
	defer remoteConn.Close()

	go okcptun.UnwrapFrames(clientConn, remoteConn)
	okcptun.WrapFrames(remoteConn, clientConn)
}
