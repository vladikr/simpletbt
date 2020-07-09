package tests

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"net"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	tbt "github.com/vladikr/simpletbt"
)

// StartTestTCPServer simply starts a test TCP server which throttles each accepted connection
func StartTestTCPServer(ctx context.Context, throttle *tbt.TransmissionBandwidthThrottle, serverAddress string) {

	tcpAddr, _ := net.ResolveTCPAddr("tcp", serverAddress)
	l, err := net.ListenTCP("tcp", tcpAddr)
	Expect(err).ToNot(HaveOccurred(), "should start test server without errors")

	go func(l *net.TCPListener, ctx context.Context) {
		for {
			select {
			case <-ctx.Done():
				By("Shutting down the tcp server")
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

				conn = throttle.ThrottleConnction(conn)
				go handleConnection(ctx, conn)
			}
		}
	}(l, ctx)
}

func handleConnection(ctx context.Context, conn net.Conn) {
	defer conn.Close()
	continuousCopy(ctx, conn)
}

type readerFunc func(p []byte) (n int, err error)

func (rf readerFunc) Read(p []byte) (n int, err error) { return rf(p) }

// continuously calling the randomReader to generate more bytes until timeout is reached.
func continuousCopy(ctx context.Context, conn net.Conn) {
	_, _ = io.Copy(conn, readerFunc(func(p []byte) (int, error) {
		for {
			select {
			case <-ctx.Done():
				return 0, fmt.Errorf("End")
			default:
				return rand.Read(p)
			}
		}
	}))
}
