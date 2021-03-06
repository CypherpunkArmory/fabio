package tcp

import (
	"io"
	"log"
	"net"
	"time"

	"github.com/fabiolb/fabio/metrics"
	"github.com/fabiolb/fabio/route"
)

// Proxy implements a generic TCP proxying handler.
type DynamicProxy struct {
	// DialTimeout sets the timeout for establishing the outbound
	// connection.
	DialTimeout time.Duration

	// Lookup returns a target host for the given request.
	// The proxy will panic if this value is nil.
	Lookup func(host string) *route.Target

	// Conn counts the number of connections.
	Conn metrics.Counter

	// ConnFail counts the failed upstream connection attempts.
	ConnFail metrics.Counter

	// Noroute counts the failed Lookup() calls.
	Noroute metrics.Counter
}

func (p *DynamicProxy) ServeTCP(in net.Conn) error {
	defer in.Close()

	if p.Conn != nil {
		p.Conn.Inc(1)
	}
	target := in.LocalAddr().String()
	t := p.Lookup(target)
	if t == nil {
		if p.Noroute != nil {
			p.Noroute.Inc(1)
		}
		return nil
	}
	addr := t.URL.Host
	log.Printf("[DEBUG]  Connection: %s incoming %s to %s: ", in.RemoteAddr(), target, addr)

	if t.AccessDeniedTCP(in) {
		return nil
	}

	out, err := net.DialTimeout("tcp", addr, p.DialTimeout)
	defer out.Close()
	if err != nil {
		log.Print("[WARN] tcp: cannot connect to upstream ", addr)
		if p.ConnFail != nil {
			p.ConnFail.Inc(1)
		}
		return err
	}

	errc := make(chan error, 2)
	cp := func(dst io.Writer, src io.Reader, c metrics.Counter) {
		errc <- copyBuffer(dst, src, c)
	}

	// rx measures the traffic to the upstream server (in <- out)
	// tx measures the traffic from the upstream server (out <- in)
	rx := metrics.DefaultRegistry.GetCounter(t.TimerName + ".rx")
	tx := metrics.DefaultRegistry.GetCounter(t.TimerName + ".tx")

	go cp(in, out, rx)
	go cp(out, in, tx)
	err = <-errc
	if err != nil && err != io.EOF {
		log.Print("[WARN]: tcp:  ", err)
		return err
	}
	return nil
}
