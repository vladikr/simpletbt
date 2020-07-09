package tests_test

import (
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	tbt "github.com/vladikr/simpletbt"
	"github.com/vladikr/simpletbt/tests"
)

const TCPServerAddress = "127.0.0.1:54321"

func measureConnBandwidth(conn net.Conn) float64 {
	start := time.Now()
	written, _ := io.Copy(ioutil.Discard, conn)
	timePast := time.Since(start).Seconds()
	return float64(written) / timePast
}

var _ = Describe("TCP Server", func() {
	Context("with throttled transmission", func() {
		var (
			bandwidthLimitPerServer     float64
			bandwidthLimitPerConnection float64
			throttle                    *tbt.TransmissionBandwidthThrottle
			cancel                      context.CancelFunc
		)
		BeforeEach(func() {
			bandwidthLimitPerServer = float64(500000)
			bandwidthLimitPerConnection = float64(100000)
			throttle = tbt.NewTransmissionBandwidthThrottle(
				float64(300),
				float64(100))
			sampleDuration := time.Now().Add(30 * time.Second)
			ctx, cancelFunc := context.WithDeadline(context.Background(), sampleDuration)
			cancel = cancelFunc
			By("starting a TCP Server")
			tests.StartTestTCPServer(ctx, throttle, TCPServerAddress)
			By("Updating Bandwidth limits")
			throttle.SetServerBandwidthLimit(bandwidthLimitPerServer)
			throttle.SetConnectionBandwidthLimit(bandwidthLimitPerConnection)
		})
		AfterEach(func() {
			//close the test server
			cancel()
		})
		connectToTCPServer := func(connectionNumber int) net.Conn {
			By(fmt.Sprintf("connecting to the TCP Server - %d", connectionNumber))
			conn, err := net.Dial("tcp", TCPServerAddress)
			Expect(err).ToNot(HaveOccurred(), "should connect to the test server without errors")
			return conn
		}

		reportSingleConnectionBandwidthMeasurment := func(conn net.Conn, bandwidthChan chan float64, cNum int) {
			defer conn.Close()

			By(fmt.Sprintf("measuring the bandwidth per connection - %d", cNum))
			bandwidth := measureConnBandwidth(conn)
			bandwidthChan <- bandwidth
		}

		getConnsBandwidthResults := func(connections int, updateConnBandwidth int) []float64 {
			var bandwidthChanlList []chan float64
			var connectionsBandwidthResults []float64
			for c := 0; c < connections; c++ {
				bandwidthChan := make(chan float64)
				bandwidthChanlList = append(bandwidthChanlList, bandwidthChan)
				conn := connectToTCPServer(c)
				go reportSingleConnectionBandwidthMeasurment(conn, bandwidthChan, c)

				if updateConnBandwidth != 0 {
					By("updating bandwidth limit for all running connections")
					time.Sleep(5 * time.Second)
					throttle.SetConnectionBandwidthLimit(200000)
				}
			}
			By("Collecting bandwidth results from all reported connections")
			for _, bwChan := range bandwidthChanlList {
				select {
				case bwVal := <-bwChan:
					connectionsBandwidthResults = append(connectionsBandwidthResults, bwVal)
				case <-time.After(35 * time.Second):
					connectionsBandwidthResults = append(connectionsBandwidthResults, 0)
				}
			}
			return connectionsBandwidthResults
		}
		It("should have a bandwidth variable within 5% range", func() {
			connections := 1
			dontUpdateBandwidth := 0
			Eventually(func() []float64 {
				return getConnsBandwidthResults(connections, dontUpdateBandwidth)
			}).Should(ContainElement(BeNumerically("~", bandwidthLimitPerConnection, bandwidthLimitPerConnection*0.05)))
		})
		It("should have a bandwidth variable within 5% range for all connections", func() {
			connections := 4
			dontUpdateBandwidth := 0
			Eventually(func() []float64 {
				return getConnsBandwidthResults(connections, dontUpdateBandwidth)
			}).Should(ContainElement(BeNumerically("~", bandwidthLimitPerConnection, bandwidthLimitPerConnection*0.05)))
		})
		It("should be able to update bandwidth limit for all connections", func() {
			connections := 4
			updatedBandwidth := 200000
			Eventually(func() []float64 {
				return getConnsBandwidthResults(connections, updatedBandwidth)
			}).ShouldNot(ContainElement(BeNumerically("~", bandwidthLimitPerConnection, bandwidthLimitPerConnection*0.05)))
		})
	})
})
