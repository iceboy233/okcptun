# okcptun

Opinionated KCP Tunnel.

The main difference is that `okcptun` multiplexes all tunneled connections onto
a single UDP connection at packet level, that hides some of the connection
characteristics without introducing head of line blocking. It also uses
AES-256-CTR with BLAKE3 SIV to encrypt the packets.

## Usage

Server:

```
server -localAddr=0.0.0.0:10000 -password=123456 -targetAddr=1.1.1.1:20000
```

Client:

```
client -localAddr=127.0.0.1:20000 -password=123456 -remoteAddr=2.2.2.2:10000
```
