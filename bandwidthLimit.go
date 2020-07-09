package simpletbt

import (
	"net"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// align burst size to 4k
const burstSize = 4096

// TransmissionBandwidthThrottle inforamtion about throttled connections
type TransmissionBandwidthThrottle struct {
	serverBandwidthLimit      *rate.Limiter
	defaultConnBandwidthLimit *rate.Limiter
	connectionLimitMap        map[net.Conn][]*rate.Limiter
	limiterLock               sync.Mutex
}

// NewTransmissionBandwidthThrottle returns an instance of TransmissionBandwidthThrottle
func NewTransmissionBandwidthThrottle(serverBandwidth float64, connBandwidth float64) *TransmissionBandwidthThrottle {
	return &TransmissionBandwidthThrottle{
		serverBandwidthLimit:      rate.NewLimiter(rate.Limit(serverBandwidth), burstSize),
		defaultConnBandwidthLimit: rate.NewLimiter(rate.Limit(connBandwidth), burstSize),
		connectionLimitMap:        make(map[net.Conn][]*rate.Limiter),
	}
}

func (tbt *TransmissionBandwidthThrottle) addConnection(conn net.Conn, limit *rate.Limiter) {
	tbt.limiterLock.Lock()
	defer tbt.limiterLock.Unlock()
	tbt.connectionLimitMap[conn] = []*rate.Limiter{limit, tbt.serverBandwidthLimit}
}

func (tbt *TransmissionBandwidthThrottle) removeConnection(conn net.Conn) {
	tbt.limiterLock.Lock()
	defer tbt.limiterLock.Unlock()
	delete(tbt.connectionLimitMap, conn)
}

// ThrottleConnction throttles the transmission of a provided connection
func (tbt *TransmissionBandwidthThrottle) ThrottleConnction(conn net.Conn) net.Conn {

	lim := tbt.defaultConnBandwidthLimit.Limit()
	burst := tbt.defaultConnBandwidthLimit.Burst()
	// create new limiter for each connection and store it so
	// SetLimits could be applied later on
	newConnBandwidthLimit := rate.NewLimiter(lim, burst)
	tbt.addConnection(conn, newConnBandwidthLimit)

	return &BandwidthLimitConn{
		Conn:           conn,
		throttleLimits: tbt,
	}
}

// SetServerBandwidthLimit sets the bandwidth limit for the server
func (tbt *TransmissionBandwidthThrottle) SetServerBandwidthLimit(serverBandwidth float64) {
	tbt.limiterLock.Lock()
	defer tbt.limiterLock.Unlock()
	tbt.serverBandwidthLimit.SetLimit(rate.Limit(serverBandwidth))
}

// SetConnectionBandwidthLimit sets/updates bandwidth limit for all existing connections
func (tbt *TransmissionBandwidthThrottle) SetConnectionBandwidthLimit(connBandwidth float64) {
	tbt.limiterLock.Lock()
	defer tbt.limiterLock.Unlock()
	tbt.defaultConnBandwidthLimit.SetLimit(rate.Limit(connBandwidth))
	for _, limiter := range tbt.connectionLimitMap {
		// connectionLimitMap is a slice of connection limit and a server limit
		// here we only need the connection limiter
		limiter[0].SetLimit(rate.Limit(connBandwidth))
	}
}

// BandwidthLimitConn is the throttled connection struct
type BandwidthLimitConn struct {
	throttleLimits *TransmissionBandwidthThrottle
	net.Conn
}

// Close override the net.Conn.Close to remove stored limiter from connections map
func (c *BandwidthLimitConn) Close() error {
	c.throttleLimits.removeConnection(c.Conn)
	return c.Conn.Close()
}

func getMinBurst(conn net.Conn, tbt *TransmissionBandwidthThrottle) int {
	tbt.limiterLock.Lock()
	defer tbt.limiterLock.Unlock()

	burst := burstSize

	// find out if one of the limiters has a lowers burst size set
	if connLimits, exist := tbt.connectionLimitMap[conn]; exist {
		for _, connLimit := range connLimits {
			if connLimit.Burst() < burst {
				burst = connLimit.Burst()
			}
		}
	}
	return burst
}

func getMaxWaitTime(conn net.Conn, tbt *TransmissionBandwidthThrottle, writeLength int) (maxDelay time.Duration) {
	tbt.limiterLock.Lock()
	defer tbt.limiterLock.Unlock()

	if limits, exist := tbt.connectionLimitMap[conn]; exist {
		// reserve & delay pattern from https://pkg.go.dev/golang.org/x/time/rate?tab=doc#Limiter.ReserveN
		// as there is no deadline to respect, this pattern is preferred
		for _, limit := range limits {
			reserved := limit.ReserveN(time.Now(), writeLength)
			if !reserved.OK() {
				return
			}
			delay := reserved.Delay()
			if delay > maxDelay {
				maxDelay = delay
			}
		}
	}
	return
}

func (c *BandwidthLimitConn) Write(b []byte) (n int, err error) {

	burst := getMinBurst(c.Conn, c.throttleLimits)
	for {
		writeLength := len(b)
		if writeLength == 0 {
			break
		}
		if burst < writeLength {
			writeLength = burst
		}

		delay := getMaxWaitTime(c.Conn, c.throttleLimits, writeLength)
		time.Sleep(delay)
		writtenSize, err := c.Conn.Write(b[:writeLength])
		if err != nil {
			return 0, err
		}
		n += writtenSize
		b = b[writeLength:]
	}
	return
}
