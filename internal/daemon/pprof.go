package daemon

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	httppprof "net/http/pprof"
	"runtime/pprof"
	"strings"
	"sync/atomic"
	"time"
)

// Profiles: with KERO_PPROF set to a loopback address, as
// KERO_PPROF=127.0.0.1:6060, kerod serves Go's profiles there
// (/debug/pprof/) while it runs, and labels the work it does often
// (profiled), so that a CPU profile tells the panes' ticks, the screen
// reads, the history saves and the frames sent to clients apart:
//
//	go tool pprof -tagfocus kerod=pane.screen http://127.0.0.1:6060/debug/pprof/profile?seconds=30
//
// Unset, kerod serves nothing and labels nothing. The address must be a
// loopback one: the profiles show what the panes hold.

// profiling tells whether kerod serves its profiles, and so labels its
// work.
var profiling atomic.Bool

// serveProfiles serves the profiles on addr, when it is set, and returns
// what stops them, which Run calls as it returns. An address kerod does
// not serve on, one not of the loopback or one in use, as by another
// daemon whose panes passed KERO_PPROF on, is logged, and kerod runs
// without.
func serveProfiles(addr string, lg *log.Logger) (stop func()) {
	if addr == "" {
		return func() {}
	}
	if err := loopbackAddr(addr); err != nil {
		lg.Printf("KERO_PPROF: %v: not serving profiles", err)
		return func() {}
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		lg.Printf("KERO_PPROF: %v: not serving profiles", err)
		return func() {}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", httppprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", httppprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", httppprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", httppprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", httppprof.Trace)
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	profiling.Store(true)
	lg.Printf("serving profiles on http://%s/debug/pprof/", ln.Addr())
	go srv.Serve(ln)
	return func() {
		profiling.Store(false)
		srv.Close()
	}
}

// loopbackAddr tells why kerod does not serve on addr, a host and a port:
// nil when the host is localhost or a loopback address. No host, as in
// ":6060", is every interface's, and refused.
func loopbackAddr(addr string) error {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return err
	}
	if port == "" {
		return fmt.Errorf("%q has no port", addr)
	}
	if strings.EqualFold(host, "localhost") {
		return nil
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return nil
	}
	return fmt.Errorf("%q is not a loopback address", addr)
}

// profiled runs f, labelled kerod=name in profiles while kerod serves
// them. Otherwise it only calls f, and costs nothing: f does not escape,
// so the closure at the call stays on the stack.
func profiled(name string, f func()) {
	if !profiling.Load() {
		f()
		return
	}
	pprof.Do(context.Background(), pprof.Labels("kerod", name), func(context.Context) { f() })
}
