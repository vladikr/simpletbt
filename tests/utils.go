package tests

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"io"
	"net"
	"time"

	. "github.com/onsi/gomega"

	"github.com/vladikr/simpletbt/pkg/tbt"
)

var transmissionTime time.Duration

// Simply starts a test TCP server which trottles each accepted connection
func StartTestTCPServer(trottle *tbt.TransmissionBandwidthTrottle, transmissionTimeInSec int, quit chan bool) {
	transmissionTime = time.Duration(transmissionTimeInSec)
	tcpAddr, _ := net.ResolveTCPAddr("tcp", "127.0.0.1:54321")
	l, err := net.ListenTCP("tcp", tcpAddr)
	Expect(err).ToNot(HaveOccurred(), "should start test server without errors")

	go func(l *net.TCPListener, quit chan bool) {
		for {
			select {
			case <-quit:
				l.Close()
				return
			default:
				l.SetDeadline(time.Now().Add(1e9))
				conn, err := l.Accept()
				if err != nil {
					if opErr, ok := err.(*net.OpError); ok && opErr.Timeout() {
						continue
					}
					Expect(err).ToNot(HaveOccurred(), "should accept connections without errors")
				}

				conn = trottle.TrottleConnction(conn)
				go handleConnection(conn)
			}
		}
	}(l, quit)
}

// generate random bytes
func randBytes(n int) []byte {
	b := make([]byte, n)
	rand.Read(b)
	return b
}

func handleConnection(conn net.Conn) {
	defer conn.Close()
	continuousCopy(conn)
}

func randomReader() *bytes.Reader {
	input := randBytes(100 * 1024)
	return bytes.NewReader(input)
}

type readerFunc func(p []byte) (n int, err error)

func (rf readerFunc) Read(p []byte) (n int, err error) { return rf(p) }

// continuously calling the randomReader to generate more bytes until timeout is reached.
func continuousCopy(conn net.Conn) {
	timeout := time.After(transmissionTime * time.Second)
	_, _ = io.Copy(conn, readerFunc(func(p []byte) (int, error) {
		for {
			select {
			case <-timeout:
				return 0, fmt.Errorf("End")
			default:
				return randomReader().Read(p)
			}
		}
	}))
}
