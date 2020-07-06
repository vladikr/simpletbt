package tests_test

import (
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/vladikr/simpletbt/pkg/tbt"
	"github.com/vladikr/simpletbt/tests"
)

func measureConnBandwidth(conn net.Conn) float64 {
	start := time.Now()
	written, _ := io.Copy(ioutil.Discard, conn)
	timePast := time.Since(start).Seconds()
	return float64(written) / timePast
}

var _ = Describe("TCP Server", func() {
	Context("with trottled transmission", func() {
		var bandwidthLimitPerServer float64
		var bandwidthLimitPerConnection float64
		var shutdownChan chan bool
		var trottle *tbt.TransmissionBandwidthTrottle
		BeforeEach(func() {
			shutdownChan = make(chan bool)
			bandwidthLimitPerServer = float64(500000)
			bandwidthLimitPerConnection = float64(100000)
			trottle = tbt.NewTransmissionBandwidthTrottle(
				float64(300),
				float64(100))
			sampleDuration := 30
			By("starting a TCP Server")
			tests.StartTestTCPServer(trottle, sampleDuration, shutdownChan)
			trottle.SetServerBandwidthLimit(bandwidthLimitPerServer)
			trottle.SetConnectionBandwidthLimit(bandwidthLimitPerConnection)
		})
		AfterEach(func() {
			//close the test server
			shutdownChan <- true
		})
		getConnsBandwidthResults := func(connections int, updateConnBandwidth int) []float64 {
			var bandwidthChanlList []chan float64
			var connectionsBandwidthResults []float64
			for c := 0; c < connections; c++ {
				bandwidthChan := make(chan float64)
				bandwidthChanlList = append(bandwidthChanlList, bandwidthChan)
				go func(bandwidthChan chan float64, cNum int) {

					By(fmt.Sprintf("connecting to the TCP Server - %d", cNum))
					conn, err := net.Dial("tcp", "127.0.0.1:54321")
					if err != nil {
						Expect(err).ToNot(HaveOccurred(), "should connect to the test server without errors")
					}
					defer conn.Close()

					// update bandwidth limit for all connections
					if updateConnBandwidth != 0 && cNum == connections-1 {
						By("updating bandwidth limit for all connections")
						time.Sleep(5 * time.Second)
						trottle.SetConnectionBandwidthLimit(200000)
					}
					By(fmt.Sprintf("measuring the bandwidth per connection - %d", cNum))
					bandwidth := measureConnBandwidth(conn)
					bandwidthChan <- bandwidth
				}(bandwidthChan, c)
			}
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
