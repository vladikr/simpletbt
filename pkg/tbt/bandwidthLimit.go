package tbt

import (
	"net"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// align burst size to 4k
const BurstSize = 4096

type TransmissionBandwidthTrottle struct {
	serverBandwidthLimit      *rate.Limiter
	defaultConnBandwidthLimit *rate.Limiter
	connectionLimitMap        map[net.Conn]*rate.Limiter
	limiterLock               sync.Mutex
}

func NewTransmissionBandwidthTrottle(serverBandwidth float64, connBandwidth float64) *TransmissionBandwidthTrottle {
	return &TransmissionBandwidthTrottle{
		serverBandwidthLimit:      rate.NewLimiter(rate.Limit(serverBandwidth), BurstSize),
		defaultConnBandwidthLimit: rate.NewLimiter(rate.Limit(connBandwidth), BurstSize),
		connectionLimitMap:        make(map[net.Conn]*rate.Limiter),
	}
}

func (tbt *TransmissionBandwidthTrottle) addConnection(conn net.Conn, limit *rate.Limiter) {
	tbt.limiterLock.Lock()
	defer tbt.limiterLock.Unlock()
	tbt.connectionLimitMap[conn] = limit
}

func (tbt *TransmissionBandwidthTrottle) removeConnection(conn net.Conn) {
	tbt.limiterLock.Lock()
	defer tbt.limiterLock.Unlock()
	delete(tbt.connectionLimitMap, conn)
}

func (tbt *TransmissionBandwidthTrottle) TrottleConnction(conn net.Conn) net.Conn {

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

func (tbt *TransmissionBandwidthTrottle) SetServerBandwidthLimit(serverBandwidth float64) {
	tbt.limiterLock.Lock()
	defer tbt.limiterLock.Unlock()
	tbt.serverBandwidthLimit.SetLimit(rate.Limit(serverBandwidth))
}

func (tbt *TransmissionBandwidthTrottle) SetConnectionBandwidthLimit(connBandwidth float64) {
	tbt.limiterLock.Lock()
	defer tbt.limiterLock.Unlock()
	tbt.defaultConnBandwidthLimit.SetLimit(rate.Limit(connBandwidth))
	for _, limiter := range tbt.connectionLimitMap {
		limiter.SetLimit(rate.Limit(connBandwidth))
	}
}

type BandwidthLimitConn struct {
	throttleLimits *TransmissionBandwidthTrottle
	net.Conn
}

// override the net.Conn.Close to remove stored limiter from connections map
func (c *BandwidthLimitConn) Close() error {
	c.throttleLimits.removeConnection(c.Conn)
	return c.Conn.Close()
}

func getMinBurst(conn net.Conn, tbt *TransmissionBandwidthTrottle) int {
	tbt.limiterLock.Lock()
	defer tbt.limiterLock.Unlock()

	burst := BurstSize

	// find out if one of the limiters has a lowers burst size set
	if tbt.serverBandwidthLimit.Burst() < burst {
		burst = tbt.serverBandwidthLimit.Burst()
	}
	if connLimit, exist := tbt.connectionLimitMap[conn]; exist {
		if connLimit.Burst() < burst {
			burst = connLimit.Burst()
		}
	}
	return burst
}

func getMaxWaitTime(conn net.Conn, tbt *TransmissionBandwidthTrottle, writeLength int) (maxDelay time.Duration) {
	tbt.limiterLock.Lock()
	defer tbt.limiterLock.Unlock()

	// construct list of limits to make the reservations against
	limits := []*rate.Limiter{tbt.serverBandwidthLimit}
	if connLimit, exist := tbt.connectionLimitMap[conn]; exist {
		limits = append(limits, connLimit)
	}
	// reserve & dealy pattern from https://pkg.go.dev/golang.org/x/time/rate?tab=doc#Limiter.ReserveN
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
