package main

import (
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
)

type benchmarkConn struct{ done chan struct{} }

func (c benchmarkConn) Read([]byte) (int, error)    { <-c.done; return 0, io.EOF }
func (c benchmarkConn) Write(p []byte) (int, error) { return len(p), nil }
func (c benchmarkConn) Close() error                { return nil }

// BenchmarkTerminalTyping isolates the daemon's mirror and the UI's frame
// work; it does not measure OS input delivery or display presentation.
func BenchmarkTerminalTyping(b *testing.B) {
	for _, size := range []struct {
		name       string
		cols, rows int
		w, h       int
	}{{"small", 80, 24, 800, 450}, {"large", 280, 70, 2400, 1200}} {
		b.Run(size.name, func(b *testing.B) {
			done := make(chan struct{})
			defer close(done)
			server, err := terminal.New(terminal.Options{Conn: benchmarkConn{done}})
			if err != nil {
				b.Fatal(err)
			}
			defer server.Close()
			client, err := terminal.New(terminal.Options{Conn: benchmarkConn{done}, ResizeByConn: true})
			if err != nil {
				b.Fatal(err)
			}
			defer client.Close()
			server.Resize(size.cols, size.rows)
			client.Resize(size.cols, size.rows)
			for i := 0; i < size.rows-2; i++ {
				server.Feed([]byte(fmt.Sprintf("\x1b[32m%04d\x1b[0m %s\r\n", i, strings.Repeat("terminal content ", size.cols/18))))
			}
			mirror, err := server.NewMirror()
			if err != nil {
				b.Fatal(err)
			}
			defer mirror.Close()
			frame, _ := mirror.Frame()
			client.Feed(frame)
			tt := ui.NewTester(func(c *ui.Context) { terminal.View(c, client).Fill() }, size.w, size.h)
			tt.Frame()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				server.Feed([]byte(fmt.Sprintf("\r\x1b[K> typed character %d", i)))
				frame, _ := mirror.Frame()
				client.Feed(frame)
				tt.Frame()
			}
		})
	}
}
