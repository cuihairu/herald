package main

import (
	"bufio"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// fakeRedis is a minimal RESP stub: just enough for go-redis to connect,
// create a consumer group, and then block forever on XREADGROUP (never
// replying emulates a blocking read on an empty stream, keeping pool
// workers quiet until ctx cancellation aborts the command).
type fakeRedis struct {
	ln    net.Listener
	mu    sync.Mutex
	conns map[net.Conn]struct{}
	done  chan struct{}
}

func newFakeRedis(t *testing.T) *fakeRedis {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	f := &fakeRedis{ln: ln, conns: make(map[net.Conn]struct{}), done: make(chan struct{})}
	t.Cleanup(f.close)
	go f.acceptLoop()
	return f
}

func (f *fakeRedis) Addr() string { return f.ln.Addr().String() }

func (f *fakeRedis) close() {
	close(f.done)
	f.mu.Lock()
	_ = f.ln.Close()
	for c := range f.conns {
		_ = c.Close()
	}
	f.conns = nil
	f.mu.Unlock()
}

func (f *fakeRedis) acceptLoop() {
	for {
		conn, err := f.ln.Accept()
		if err != nil {
			return
		}
		f.mu.Lock()
		if f.conns == nil {
			f.mu.Unlock()
			_ = conn.Close()
			return
		}
		f.conns[conn] = struct{}{}
		f.mu.Unlock()
		go f.serveConn(conn)
	}
}

func (f *fakeRedis) serveConn(conn net.Conn) {
	defer func() {
		f.mu.Lock()
		if f.conns != nil {
			delete(f.conns, conn)
		}
		f.mu.Unlock()
		_ = conn.Close()
	}()
	r := bufio.NewReader(conn)
	w := bufio.NewWriter(conn)
	for {
		args, err := readRespCommand(r)
		if err != nil {
			return
		}
		if err := f.handle(w, args); err != nil {
			return
		}
		if err := w.Flush(); err != nil {
			return
		}
	}
}

func (f *fakeRedis) handle(w *bufio.Writer, args []string) error {
	switch strings.ToUpper(args[0]) {
	case "PING":
		_, err := io.WriteString(w, "+PONG\r\n")
		return err
	case "HELLO":
		// go-redis falls back to RESP2 when HELLO is unsupported.
		_, err := io.WriteString(w, "-ERR unknown command 'HELLO'\r\n")
		return err
	case "XGROUP":
		_, err := io.WriteString(w, "+OK\r\n")
		return err
	case "XREADGROUP":
		// Never reply: emulates a blocking XREADGROUP on an empty stream.
		<-f.done
		return io.EOF
	default:
		_, err := io.WriteString(w, "-ERR unknown command\r\n")
		return err
	}
}

func readRespCommand(r *bufio.Reader) ([]string, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		return nil, err
	}
	line = strings.TrimRight(line, "\r\n")
	if len(line) == 0 || line[0] != '*' {
		return nil, io.EOF
	}
	n, err := strconv.Atoi(line[1:])
	if err != nil {
		return nil, err
	}
	args := make([]string, 0, n)
	for i := 0; i < n; i++ {
		hdr, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		hdr = strings.TrimRight(hdr, "\r\n")
		if len(hdr) < 1 || hdr[0] != '$' {
			return nil, io.EOF
		}
		size, err := strconv.Atoi(hdr[1:])
		if err != nil {
			return nil, err
		}
		buf := make([]byte, size+2) // payload + CRLF
		if _, err := io.ReadFull(r, buf); err != nil {
			return nil, err
		}
		args = append(args, string(buf[:size]))
	}
	return args, nil
}
