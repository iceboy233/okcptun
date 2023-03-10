package main

import (
	"flag"
	"io"
	"log"
	"net"
	"okcptun"

	"github.com/xtaci/kcp-go/v5"
)

var (
	flagLocalAddr  = flag.String("localAddr", "127.0.0.1:10000", "")
	flagTargetAddr = flag.String("targetAddr", "127.0.0.1:10000", "")
	flagPassword   = flag.String("password", "", "")
)

func main() {
	flag.Parse()

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
	mux := okcptun.NewKCPMux(conn, *flagPassword, true)
	for {
		clientConn, closer := mux.Accept()
		go handle(clientConn, closer, targetAddr)
	}
}

func handle(
	clientConn *kcp.UDPSession, closer io.Closer, targetAddr *net.TCPAddr) {
	defer closer.Close()
	defer clientConn.Close()

	log.Print("handle: new connection from ", clientConn.RemoteAddr())
	targetConn, err := net.DialTCP("tcp", nil, targetAddr)
	if err != nil {
		log.Print("handle: DialTCP failed: ", err)
		return
	}
	defer targetConn.Close()

	go okcptun.UnwrapFrames(targetConn, clientConn)
	okcptun.WrapFrames(clientConn, targetConn)
}
