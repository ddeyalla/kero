package linear

import (
	"errors"
	"sync"
	"time"

	"kero/internal/client"
	"kero/internal/proto"
)

// Daemon is kerod as the plugin sees it: the state it sends whole on every
// change, and its calls. The plugin never starts a daemon, and never reads
// the Linear key: it asks kerod, which holds it.
type Daemon interface {
	// Watch delivers the daemon's state, and tells when the daemon is out
	// of reach (a nil state and the reason) and back. It returns at once;
	// the deliveries come on the daemon's own goroutine.
	Watch(fn func(st *proto.State, down string))
	// Call calls a method of the daemon, decoding its result into out.
	Call(method string, params, out any) error
	// Close ends the watch.
	Close()
}

// errDown is what a call reads as while the daemon is out of reach.
var errDown = errors.New("the Shibu service is not reachable")

// liveDaemon talks to the real kerod, on the control connection the app
// itself uses, without starting it.
type liveDaemon struct {
	mu     sync.Mutex
	cl     *client.Client
	stop   chan struct{}
	closed bool
}

// retryAfter is how long a lost or refused connection waits before the next
// try, growing to retryMax.
const (
	retryAfter = time.Second
	retryMax   = 15 * time.Second
)

func (d *liveDaemon) Watch(fn func(st *proto.State, down string)) {
	d.mu.Lock()
	if d.stop != nil {
		d.mu.Unlock()
		return
	}
	d.stop = make(chan struct{})
	stop := d.stop
	d.mu.Unlock()
	go d.loop(fn, stop)
}

func (d *liveDaemon) loop(fn func(st *proto.State, down string), stop chan struct{}) {
	wait := retryAfter
	for {
		cl, err := client.Connect(true)
		if err != nil {
			fn(nil, err.Error())
		} else {
			wait = retryAfter
			d.mu.Lock()
			d.cl = cl
			d.mu.Unlock()
			cl.OnEvent(func(m proto.Message) {
				if m.Event == "state" && m.State != nil {
					fn(m.State, "")
				}
			})
			cl.Go("subscribe", nil, nil)
			select {
			case <-cl.Done():
				d.mu.Lock()
				d.cl = nil
				d.mu.Unlock()
				fn(nil, errDown.Error())
			case <-stop:
				cl.Close()
				return
			}
		}
		select {
		case <-time.After(wait):
			wait = min(wait*2, retryMax)
		case <-stop:
			return
		}
	}
}

func (d *liveDaemon) Call(method string, params, out any) error {
	d.mu.Lock()
	cl := d.cl
	d.mu.Unlock()
	if cl == nil {
		return errDown
	}
	return cl.Call(method, params, out)
}

func (d *liveDaemon) Close() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.stop != nil && !d.closed {
		d.closed = true
		close(d.stop)
	}
}
