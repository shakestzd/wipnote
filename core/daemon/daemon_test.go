package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// newTestListener starts a Listener on a temp-dir socket with the given
// Reader. It returns the listener, the socket path, and a cleanup func.
func newTestListener(t *testing.T, reader Reader) (*Listener, string, func()) {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".wipnote"), 0o755); err != nil {
		t.Fatalf("mkdir .wipnote: %v", err)
	}
	sock := SocketPath(dir)
	ln, err := NewListener(ListenerConfig{SocketPath: sock, Reader: reader})
	if err != nil {
		t.Fatalf("new listener: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = ln.Serve(ctx) }()
	// Wait for the socket inode to appear so the first dial doesn't race the bind.
	waitForSocket(t, sock)
	return ln, sock, func() { cancel(); _ = ln.Close() }
}

func waitForSocket(t *testing.T, sock string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(sock); err == nil {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("socket %s never appeared", sock)
}

// TestWriteFrameRefusedReadServed pins the read-only daemon (feat-174dc958):
// a write envelope, as an older wipnote binary sends one, gets an error ack
// and applies nothing, while a read on the same connection is still served.
func TestWriteFrameRefusedReadServed(t *testing.T) {
	_, sock, cleanup := newTestListener(t, nil)
	defer cleanup()
	conn, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	r := bufio.NewReader(conn)

	write := `{"op_format_version":2,"op_id":"w1","op_type":"sql.exec","payload":"e30="}` + "\n"
	if _, err := conn.Write([]byte(write)); err != nil {
		t.Fatal(err)
	}
	var ack Ack
	if line, err := r.ReadBytes('\n'); err != nil || json.Unmarshal(line, &ack) != nil {
		t.Fatalf("write reply: %q err=%v", line, err)
	}
	if ack.Status != AckError || ack.Error != writesNotServed {
		t.Fatalf("write ack = %+v, want the writes-not-served error", ack)
	}

	read := `{"read_op":"ping","read_format_version":` + jsonInt(ReadFormatVersion) + `}` + "\n"
	if _, err := conn.Write([]byte(read)); err != nil {
		t.Fatal(err)
	}
	var resp ReadResponse
	if line, err := r.ReadBytes('\n'); err != nil || json.Unmarshal(line, &resp) != nil {
		t.Fatalf("read reply: %q err=%v", line, err)
	}
	if resp.ReadStatus != ReadStatusOK {
		t.Fatalf("ping after a refused write = %+v, want ok", resp)
	}
}

func jsonInt(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}
