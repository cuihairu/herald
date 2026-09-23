package queue

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cuihairu/herald/core"
)

type fakeEntry struct {
	id     string
	values []string
}

type fakeRedis struct {
	t     *testing.T
	ln    net.Listener
	mu    sync.Mutex
	conns map[net.Conn]struct{}

	entries []fakeEntry
	seq     int
	groups  map[string]int

	failXGroup bool
	failXAdd   bool
	failXAck   bool
	emptyXRead bool
	closed     bool
}

func newFakeRedis(t *testing.T) *fakeRedis {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	f := &fakeRedis{
		t:      t,
		ln:     ln,
		conns:  make(map[net.Conn]struct{}),
		groups: make(map[string]int),
	}
	t.Cleanup(f.stop)
	go f.acceptLoop()
	return f
}

func (f *fakeRedis) Addr() string {
	return f.ln.Addr().String()
}

func (f *fakeRedis) stop() {
	f.mu.Lock()
	if f.closed {
		f.mu.Unlock()
		return
	}
	f.closed = true
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
		if f.closed {
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
		delete(f.conns, conn)
		f.mu.Unlock()
		_ = conn.Close()
	}()
	r := bufio.NewReader(conn)
	w := bufio.NewWriter(conn)
	for {
		args, err := readCommand(r)
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

func readCommand(r *bufio.Reader) ([]string, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		return nil, err
	}
	line = strings.TrimRight(line, "\r\n")
	if len(line) == 0 || line[0] != '*' {
		return nil, fmt.Errorf("unexpected line %q", line)
	}
	n, err := strconv.Atoi(line[1:])
	if err != nil {
		return nil, err
	}
	args := make([]string, 0, n)
	for i := 0; i < n; i++ {
		h, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		h = strings.TrimRight(h, "\r\n")
		if len(h) == 0 || h[0] != '$' {
			return nil, fmt.Errorf("unexpected bulk header %q", h)
		}
		l, err := strconv.Atoi(h[1:])
		if err != nil {
			return nil, err
		}
		buf := make([]byte, l+2)
		if _, err := io.ReadFull(r, buf); err != nil {
			return nil, err
		}
		args = append(args, string(buf[:l]))
	}
	return args, nil
}

func writeSimple(w *bufio.Writer, s string) {
	fmt.Fprintf(w, "+%s\r\n", s)
}

func writeError(w *bufio.Writer, s string) {
	fmt.Fprintf(w, "-%s\r\n", s)
}

func writeInt(w *bufio.Writer, i int) {
	fmt.Fprintf(w, ":%d\r\n", i)
}

func writeBulk(w *bufio.Writer, s string) {
	fmt.Fprintf(w, "$%d\r\n%s\r\n", len(s), s)
}

func writeArrayLen(w *bufio.Writer, n int) {
	fmt.Fprintf(w, "*%d\r\n", n)
}

func writeEntry(w *bufio.Writer, e fakeEntry) {
	writeArrayLen(w, 2)
	writeBulk(w, e.id)
	writeArrayLen(w, len(e.values))
	for _, v := range e.values {
		writeBulk(w, v)
	}
}

func entrySeq(id string) int {
	i := strings.IndexByte(id, '-')
	if i < 0 {
		i = len(id)
	}
	n, _ := strconv.Atoi(id[:i])
	return n
}

func (f *fakeRedis) handle(w *bufio.Writer, args []string) error {
	if len(args) == 0 {
		writeError(w, "ERR empty command")
		return nil
	}
	switch strings.ToLower(args[0]) {
	case "hello":
		writeError(w, "ERR unknown command 'HELLO'")
	case "client":
		writeSimple(w, "OK")
	case "ping":
		writeSimple(w, "PONG")
	case "xgroup":
		return f.handleXGroup(w, args)
	case "xadd":
		return f.handleXAdd(w, args)
	case "xreadgroup":
		return f.handleXReadGroup(w, args)
	case "xack":
		return f.handleXAck(w, args)
	case "xrange":
		return f.handleXRange(w, args)
	case "xinfo":
		return f.handleXInfo(w, args)
	default:
		writeError(w, "ERR unknown command '"+args[0]+"'")
	}
	return nil
}

func (f *fakeRedis) handleXGroup(w *bufio.Writer, args []string) error {
	f.mu.Lock()
	fail := f.failXGroup
	f.mu.Unlock()
	if fail {
		writeError(w, "ERR XGROUP failed")
		return nil
	}
	if len(args) < 5 || strings.ToLower(args[1]) != "create" {
		writeError(w, "ERR unsupported XGROUP subcommand")
		return nil
	}
	group := args[3]
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.groups[group]; ok {
		writeError(w, "BUSYGROUP Consumer Group name already exists")
		return nil
	}
	f.groups[group] = 0
	writeSimple(w, "OK")
	return nil
}

func (f *fakeRedis) handleXAdd(w *bufio.Writer, args []string) error {
	f.mu.Lock()
	fail := f.failXAdd
	f.mu.Unlock()
	if fail {
		writeError(w, "ERR XADD failed")
		return nil
	}
	f.mu.Lock()
	f.seq++
	e := fakeEntry{id: fmt.Sprintf("%d-1", f.seq), values: append([]string(nil), args[3:]...)}
	f.entries = append(f.entries, e)
	f.mu.Unlock()
	writeBulk(w, e.id)
	return nil
}

func (f *fakeRedis) handleXReadGroup(w *bufio.Writer, args []string) error {
	streamsIdx := -1
	group := ""
	count := int(1 << 62)
	for i := 1; i < len(args); i++ {
		switch strings.ToLower(args[i]) {
		case "group":
			if i+2 < len(args) {
				group = args[i+1]
			}
		case "count":
			if i+1 < len(args) {
				if n, err := strconv.Atoi(args[i+1]); err == nil {
					count = n
				}
			}
		case "streams":
			streamsIdx = i
		}
	}
	if streamsIdx < 0 || streamsIdx+2 >= len(args) {
		writeError(w, "ERR bad XREADGROUP")
		return nil
	}

	f.mu.Lock()
	if f.emptyXRead {
		f.emptyXRead = false
		f.mu.Unlock()
		writeArrayLen(w, 0)
		return nil
	}
	cursor, ok := f.groups[group]
	if !ok {
		f.mu.Unlock()
		writeError(w, "NOGROUP no such group")
		return nil
	}
	if cursor >= len(f.entries) {
		f.mu.Unlock()
		time.Sleep(20 * time.Millisecond)
		writeArrayLen(w, -1)
		return nil
	}
	avail := len(f.entries) - cursor
	if count < avail {
		avail = count
	}
	delivered := make([]fakeEntry, avail)
	copy(delivered, f.entries[cursor:cursor+avail])
	f.groups[group] = cursor + avail
	f.mu.Unlock()

	writeArrayLen(w, 1)
	writeArrayLen(w, 2)
	writeBulk(w, args[streamsIdx+1])
	writeArrayLen(w, len(delivered))
	for _, e := range delivered {
		writeEntry(w, e)
	}
	return nil
}

func (f *fakeRedis) handleXAck(w *bufio.Writer, args []string) error {
	f.mu.Lock()
	fail := f.failXAck
	f.mu.Unlock()
	if fail {
		writeError(w, "ERR XACK failed")
		return nil
	}
	writeInt(w, 1)
	return nil
}

func (f *fakeRedis) handleXRange(w *bufio.Writer, args []string) error {
	if len(args) < 4 {
		writeError(w, "ERR bad XRANGE")
		return nil
	}
	start, end := args[2], args[3]
	lo, hi := 0, int(1<<62)
	if start != "-" {
		lo = entrySeq(start)
	}
	if end != "+" {
		hi = entrySeq(end)
	}
	f.mu.Lock()
	matched := make([]fakeEntry, 0, 1)
	for _, e := range f.entries {
		s := entrySeq(e.id)
		if s >= lo && s <= hi {
			matched = append(matched, e)
		}
	}
	f.mu.Unlock()
	writeArrayLen(w, len(matched))
	for _, e := range matched {
		writeEntry(w, e)
	}
	return nil
}

func (f *fakeRedis) handleXInfo(w *bufio.Writer, args []string) error {
	if len(args) < 3 || strings.ToLower(args[1]) != "stream" {
		writeError(w, "ERR bad XINFO")
		return nil
	}
	f.mu.Lock()
	n := len(f.entries)
	f.mu.Unlock()
	writeArrayLen(w, 2)
	writeBulk(w, "length")
	writeInt(w, n)
	return nil
}

func (f *fakeRedis) addRaw(values ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seq++
	f.entries = append(f.entries, fakeEntry{
		id:     fmt.Sprintf("%d-1", f.seq),
		values: append([]string(nil), values...),
	})
}

func (f *fakeRedis) setFailFlags(xgroup, xadd, xack bool, emptyXRead bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failXGroup = xgroup
	f.failXAdd = xadd
	f.failXAck = xack
	f.emptyXRead = emptyXRead
}

func newTestRedisQueue(t *testing.T, fr *fakeRedis) core.Queue {
	t.Helper()
	q, err := NewRedisQueue(&QueueConfig{Redis: RedisConfig{Addr: fr.Addr()}})
	if err != nil {
		t.Fatalf("failed to create redis queue: %v", err)
	}
	t.Cleanup(func() { _ = q.Close() })
	return q
}

func TestNewRedisQueueWithServer(t *testing.T) {
	t.Run("creates queue with defaults", func(t *testing.T) {
		fr := newFakeRedis(t)
		q, err := NewRedisQueue(&QueueConfig{Redis: RedisConfig{Addr: fr.Addr()}})
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		defer func() { _ = q.Close() }()

		rq, ok := q.(*redisQueue)
		if !ok {
			t.Fatalf("expected *redisQueue, got %T", q)
		}
		if rq.stream != "herald:tasks" {
			t.Errorf("expected default stream herald:tasks, got %s", rq.stream)
		}
		if rq.group != "herald-workers" {
			t.Errorf("expected default group herald-workers, got %s", rq.group)
		}
		if !strings.HasPrefix(rq.consumer, "worker-") {
			t.Errorf("expected consumer prefix worker-, got %s", rq.consumer)
		}
		if rq.pending == nil {
			t.Error("expected non-nil pending map")
		}
	})

	t.Run("reuses existing consumer group", func(t *testing.T) {
		fr := newFakeRedis(t)
		q1, err := NewRedisQueue(&QueueConfig{Redis: RedisConfig{Addr: fr.Addr()}})
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		defer func() { _ = q1.Close() }()

		q2, err := NewRedisQueue(&QueueConfig{Redis: RedisConfig{Addr: fr.Addr()}})
		if err != nil {
			t.Fatalf("expected BUSYGROUP to be tolerated, got %v", err)
		}
		_ = q2.Close()
	})

	t.Run("custom stream and group", func(t *testing.T) {
		fr := newFakeRedis(t)
		q, err := NewRedisQueue(&QueueConfig{Redis: RedisConfig{
			Addr:   fr.Addr(),
			Stream: "custom-stream",
			Group:  "custom-group",
		}})
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		defer func() { _ = q.Close() }()

		rq := q.(*redisQueue)
		if rq.stream != "custom-stream" {
			t.Errorf("expected custom-stream, got %s", rq.stream)
		}
		if rq.group != "custom-group" {
			t.Errorf("expected custom-group, got %s", rq.group)
		}
	})

	t.Run("connection failure returns error", func(t *testing.T) {
		fr := newFakeRedis(t)
		addr := fr.Addr()
		fr.stop()

		_, err := NewRedisQueue(&QueueConfig{Redis: RedisConfig{Addr: addr}})
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "failed to connect to redis") {
			t.Errorf("expected connect error, got %v", err)
		}
	})

	t.Run("group creation failure returns error", func(t *testing.T) {
		fr := newFakeRedis(t)
		fr.setFailFlags(true, false, false, false)

		_, err := NewRedisQueue(&QueueConfig{Redis: RedisConfig{Addr: fr.Addr()}})
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "failed to create consumer group") {
			t.Errorf("expected group creation error, got %v", err)
		}
	})
}

func TestRedisQueuePushPopWithServer(t *testing.T) {
	t.Run("push and pop roundtrip", func(t *testing.T) {
		fr := newFakeRedis(t)
		q := newTestRedisQueue(t, fr)

		task := &core.DeliveryTask{
			ID:       "task-1",
			Provider: "email",
			Targets:  []string{"user@example.com"},
		}
		ctx := context.Background()

		if err := q.Push(ctx, task); err != nil {
			t.Errorf("expected no error pushing, got %v", err)
		}

		popped, err := q.Pop(ctx)
		if err != nil {
			t.Fatalf("expected no error popping, got %v", err)
		}
		if popped.ID != task.ID {
			t.Errorf("expected ID %s, got %s", task.ID, popped.ID)
		}
		if popped.Provider != task.Provider {
			t.Errorf("expected Provider %s, got %s", task.Provider, popped.Provider)
		}
		if len(popped.Targets) != 1 || popped.Targets[0] != "user@example.com" {
			t.Errorf("expected targets to roundtrip, got %v", popped.Targets)
		}
	})

	t.Run("multiple push and pop in FIFO order", func(t *testing.T) {
		fr := newFakeRedis(t)
		q := newTestRedisQueue(t, fr)
		ctx := context.Background()

		for i := 0; i < 3; i++ {
			if err := q.Push(ctx, &core.DeliveryTask{ID: fmt.Sprintf("task-%d", i), Provider: "email"}); err != nil {
				t.Fatalf("expected no error pushing task %d, got %v", i, err)
			}
		}
		for i := 0; i < 3; i++ {
			popped, err := q.Pop(ctx)
			if err != nil {
				t.Fatalf("expected no error popping task %d, got %v", i, err)
			}
			want := fmt.Sprintf("task-%d", i)
			if popped.ID != want {
				t.Errorf("expected ID %s, got %s", want, popped.ID)
			}
		}
	})

	t.Run("pop empty queue returns no messages error", func(t *testing.T) {
		fr := newFakeRedis(t)
		q := newTestRedisQueue(t, fr)

		_, err := q.Pop(context.Background())
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if err.Error() != "no messages available" {
			t.Errorf("expected 'no messages available', got %v", err)
		}
	})

	t.Run("pop with empty streams reply returns no messages error", func(t *testing.T) {
		fr := newFakeRedis(t)
		q := newTestRedisQueue(t, fr)
		fr.setFailFlags(false, false, false, true)

		_, err := q.Pop(context.Background())
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if err.Error() != "no messages available" {
			t.Errorf("expected 'no messages available', got %v", err)
		}
	})

	t.Run("pop with cancelled context returns context error", func(t *testing.T) {
		fr := newFakeRedis(t)
		q := newTestRedisQueue(t, fr)

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := q.Pop(ctx)
		if err != context.Canceled {
			t.Errorf("expected context.Canceled, got %v", err)
		}
	})

	t.Run("push to closed queue returns ErrQueueClosed", func(t *testing.T) {
		fr := newFakeRedis(t)
		q := newTestRedisQueue(t, fr)
		_ = q.Close()

		err := q.Push(context.Background(), &core.DeliveryTask{ID: "task-1", Provider: "email"})
		if err != ErrQueueClosed {
			t.Errorf("expected ErrQueueClosed, got %v", err)
		}
	})

	t.Run("pop from closed queue returns ErrQueueClosed", func(t *testing.T) {
		fr := newFakeRedis(t)
		q := newTestRedisQueue(t, fr)
		_ = q.Close()

		_, err := q.Pop(context.Background())
		if err != ErrQueueClosed {
			t.Errorf("expected ErrQueueClosed, got %v", err)
		}
	})

	t.Run("push returns error when server unavailable", func(t *testing.T) {
		fr := newFakeRedis(t)
		q := newTestRedisQueue(t, fr)
		fr.stop()

		err := q.Push(context.Background(), &core.DeliveryTask{ID: "task-1", Provider: "email"})
		if err == nil {
			t.Error("expected error, got nil")
		}
	})

	t.Run("pop returns error when server unavailable", func(t *testing.T) {
		fr := newFakeRedis(t)
		q := newTestRedisQueue(t, fr)
		fr.stop()

		_, err := q.Pop(context.Background())
		if err == nil {
			t.Error("expected error, got nil")
		}
	})
}

func TestRedisQueueAckWithServer(t *testing.T) {
	t.Run("ack popped task succeeds", func(t *testing.T) {
		fr := newFakeRedis(t)
		q := newTestRedisQueue(t, fr)
		ctx := context.Background()

		_ = q.Push(ctx, &core.DeliveryTask{ID: "task-1", Provider: "email"})
		popped, err := q.Pop(ctx)
		if err != nil {
			t.Fatalf("expected no error popping, got %v", err)
		}
		if err := q.Ack(ctx, popped.ID); err != nil {
			t.Errorf("expected no error acking, got %v", err)
		}
	})

	t.Run("ack is idempotent", func(t *testing.T) {
		fr := newFakeRedis(t)
		q := newTestRedisQueue(t, fr)
		ctx := context.Background()

		_ = q.Push(ctx, &core.DeliveryTask{ID: "task-1", Provider: "email"})
		popped, _ := q.Pop(ctx)

		if err := q.Ack(ctx, popped.ID); err != nil {
			t.Fatalf("expected no error first ack, got %v", err)
		}
		if err := q.Ack(ctx, popped.ID); err != nil {
			t.Errorf("expected no error second ack, got %v", err)
		}
	})

	t.Run("ack non-existent task returns nil", func(t *testing.T) {
		fr := newFakeRedis(t)
		q := newTestRedisQueue(t, fr)

		if err := q.Ack(context.Background(), "non-existent"); err != nil {
			t.Errorf("expected no error, got %v", err)
		}
	})

	t.Run("ack returns error when xack fails", func(t *testing.T) {
		fr := newFakeRedis(t)
		q := newTestRedisQueue(t, fr)
		ctx := context.Background()

		_ = q.Push(ctx, &core.DeliveryTask{ID: "task-1", Provider: "email"})
		popped, _ := q.Pop(ctx)

		fr.setFailFlags(false, false, true, false)
		if err := q.Ack(ctx, popped.ID); err == nil {
			t.Error("expected error, got nil")
		}
	})
}

func TestRedisQueueNackWithServer(t *testing.T) {
	t.Run("nack requeues message", func(t *testing.T) {
		fr := newFakeRedis(t)
		q := newTestRedisQueue(t, fr)
		ctx := context.Background()

		_ = q.Push(ctx, &core.DeliveryTask{ID: "task-1", Provider: "email"})
		popped, err := q.Pop(ctx)
		if err != nil {
			t.Fatalf("expected no error popping, got %v", err)
		}

		if err := q.Nack(ctx, popped.ID, fmt.Errorf("delivery failed")); err != nil {
			t.Fatalf("expected no error nacking, got %v", err)
		}

		again, err := q.Pop(ctx)
		if err != nil {
			t.Fatalf("expected no error popping requeued task, got %v", err)
		}
		if again.ID != popped.ID {
			t.Errorf("expected requeued ID %s, got %s", popped.ID, again.ID)
		}
	})

	t.Run("nack non-existent task returns nil", func(t *testing.T) {
		fr := newFakeRedis(t)
		q := newTestRedisQueue(t, fr)

		if err := q.Nack(context.Background(), "non-existent", fmt.Errorf("failed")); err != nil {
			t.Errorf("expected no error, got %v", err)
		}
	})

	t.Run("nack returns error when message cannot be loaded", func(t *testing.T) {
		fr := newFakeRedis(t)
		q := newTestRedisQueue(t, fr)
		ctx := context.Background()

		_ = q.Push(ctx, &core.DeliveryTask{ID: "task-1", Provider: "email"})
		popped, _ := q.Pop(ctx)

		fr.stop()
		err := q.Nack(ctx, popped.ID, fmt.Errorf("failed"))
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "failed to load message for retry") {
			t.Errorf("expected load error, got %v", err)
		}
	})

	t.Run("nack returns error when requeue fails", func(t *testing.T) {
		fr := newFakeRedis(t)
		q := newTestRedisQueue(t, fr)
		ctx := context.Background()

		_ = q.Push(ctx, &core.DeliveryTask{ID: "task-1", Provider: "email"})
		popped, _ := q.Pop(ctx)

		fr.setFailFlags(false, true, false, false)
		err := q.Nack(ctx, popped.ID, fmt.Errorf("failed"))
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "failed to requeue message") {
			t.Errorf("expected requeue error, got %v", err)
		}
	})

	t.Run("nack returns error when ack of nacked message fails", func(t *testing.T) {
		fr := newFakeRedis(t)
		q := newTestRedisQueue(t, fr)
		ctx := context.Background()

		_ = q.Push(ctx, &core.DeliveryTask{ID: "task-1", Provider: "email"})
		popped, _ := q.Pop(ctx)

		fr.setFailFlags(false, false, true, false)
		err := q.Nack(ctx, popped.ID, fmt.Errorf("failed"))
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "failed to ack nacked message") {
			t.Errorf("expected ack error, got %v", err)
		}
	})
}

func TestRedisQueueSizeWithServer(t *testing.T) {
	t.Run("size reflects stream length", func(t *testing.T) {
		fr := newFakeRedis(t)
		q := newTestRedisQueue(t, fr)
		ctx := context.Background()

		if q.Size() != 0 {
			t.Errorf("expected size 0, got %d", q.Size())
		}
		for i := 0; i < 3; i++ {
			_ = q.Push(ctx, &core.DeliveryTask{ID: fmt.Sprintf("task-%d", i), Provider: "email"})
		}
		if q.Size() != 3 {
			t.Errorf("expected size 3, got %d", q.Size())
		}
	})

	t.Run("size returns 0 on error", func(t *testing.T) {
		fr := newFakeRedis(t)
		q := newTestRedisQueue(t, fr)
		_ = q.Push(context.Background(), &core.DeliveryTask{ID: "task-1", Provider: "email"})

		fr.stop()
		if q.Size() != 0 {
			t.Errorf("expected size 0 on error, got %d", q.Size())
		}
	})
}

func TestRedisQueueCloseWithServer(t *testing.T) {
	t.Run("close is idempotent", func(t *testing.T) {
		fr := newFakeRedis(t)
		q := newTestRedisQueue(t, fr)

		if err := q.Close(); err != nil {
			t.Errorf("expected no error on first close, got %v", err)
		}
		if err := q.Close(); err != nil {
			t.Errorf("expected no error on second close, got %v", err)
		}
	})

	t.Run("operations after close", func(t *testing.T) {
		fr := newFakeRedis(t)
		q := newTestRedisQueue(t, fr)
		_ = q.Close()

		if _, err := q.Pop(context.Background()); err != ErrQueueClosed {
			t.Errorf("expected ErrQueueClosed from Pop, got %v", err)
		}
		if err := q.Push(context.Background(), &core.DeliveryTask{ID: "task-1"}); err != ErrQueueClosed {
			t.Errorf("expected ErrQueueClosed from Push, got %v", err)
		}
	})
}

func TestRedisQueueDecodeWithServer(t *testing.T) {
	t.Run("missing data field returns error", func(t *testing.T) {
		fr := newFakeRedis(t)
		q := newTestRedisQueue(t, fr)

		fr.addRaw("task_id", "raw-task")

		_, err := q.Pop(context.Background())
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "invalid message format") {
			t.Errorf("expected format error, got %v", err)
		}
	})

	t.Run("invalid json data returns error", func(t *testing.T) {
		fr := newFakeRedis(t)
		q := newTestRedisQueue(t, fr)

		fr.addRaw("task_id", "raw-task", "data", "{not valid json")

		_, err := q.Pop(context.Background())
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "failed to unmarshal task") {
			t.Errorf("expected unmarshal error, got %v", err)
		}
	})
}
