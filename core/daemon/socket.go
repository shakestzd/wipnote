package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// Listener serves the per-project read protocol on a Unix socket: work-item
// reads answered from canonical state, plus the transport ops (ping, attach)
// the launcher uses. It applies no writes (feat-174dc958).
type Listener struct {
	ln       net.Listener
	sockPath string

	// lease and sockInfo are what this listener owns: the writer lease (nil
	// for a fixture) and the socket inode it bound. See socket_life.go.
	lease    *Lease
	sockInfo fs.FileInfo

	// reader answers read-protocol frames (feat-f6759e37). Nil means every
	// read is answered ReadStatusUnsupported — never an empty result, so a
	// client can tell "no read path here" from "nothing matched".
	reader Reader

	// attachState tracks launcher pids that suppress idle-exit. See
	// socket_read.go for why the launcher's guarantee needs it.
	attachState

	seq atomic.Int64

	closeOnce sync.Once
	wg        sync.WaitGroup

	// Idle-exit accounting (feat-075c110d lifecycle hardening). lastActivity
	// is the UnixNano of the most recent request; inFlight counts requests
	// currently between accept and reply. ServeWithIdleTimeout exits only when
	// BOTH no request has arrived within the idle window AND inFlight == 0.
	lastActivity atomic.Int64
	inFlight     atomic.Int64
}

// ListenerConfig configures a Listener. SocketPath is the Unix socket the
// listener binds (caller derives it via SocketPath(projectRoot)).
type ListenerConfig struct {
	SocketPath string

	// Lease is the writer lease this process holds (bug-5ca53f70). Only the
	// lease owner may bind the socket: NewListener refuses when Lease no
	// longer names this process, and the listener exits once it stops doing
	// so. A nil Lease is for in-process fixtures: it binds only when nobody
	// holds the project's lease.
	Lease *Lease

	// Reader answers read-protocol frames (feat-f6759e37). Optional: a nil
	// Reader answers every read ReadStatusUnsupported.
	Reader Reader
}

// NewListener binds the Unix socket and returns a Listener ready to Serve.
//
// The lease, not the socket inode, is the ownership authority. Before it
// binds, unlinkStaleSocket confirms this process may bind at all (it holds
// cfg.Lease, or nobody holds the lease) and removes a leftover socket file,
// since a Unix-socket bind fails with EADDRINUSE while the path exists.
// Go's unlink-on-close is turned off: Close removes the path only while it is
// still the inode this listener bound, so a server that lost its socket can
// never delete its successor's.
func NewListener(cfg ListenerConfig) (*Listener, error) {
	if err := unlinkStaleSocket(cfg.SocketPath, cfg.Lease); err != nil {
		return nil, err
	}
	ln, err := net.Listen("unix", cfg.SocketPath)
	if err != nil {
		return nil, fmt.Errorf("listen unix %s: %w", cfg.SocketPath, err)
	}
	if ul, ok := ln.(*net.UnixListener); ok {
		ul.SetUnlinkOnClose(false)
	}
	sockInfo, _ := leaseFS.Stat(cfg.SocketPath)
	l := &Listener{
		ln:       ln,
		reader:   cfg.Reader,
		sockPath: cfg.SocketPath,
		lease:    cfg.Lease,
		sockInfo: sockInfo,
	}
	l.lastActivity.Store(time.Now().UnixNano())
	return l, nil
}

// Serve accepts connections until the listener is closed or ctx is done.
// Each connection is handled on its own goroutine; a connection may carry
// multiple frames (one reply per frame) so callers can reuse a dial.
func (l *Listener) Serve(ctx context.Context) error {
	go func() {
		<-ctx.Done()
		_ = l.Close()
	}()
	for {
		conn, err := l.ln.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				// Wait for Close to finish removing our socket file, so the
				// path is gone by the time Serve returns.
				_ = l.Close()
				l.wg.Wait()
				return nil
			}
			return fmt.Errorf("accept: %w", err)
		}
		l.wg.Add(1)
		go func() {
			defer l.wg.Done()
			l.handleConn(ctx, conn)
		}()
	}
}

// handleConn reads newline-delimited frames and writes one reply per frame.
// The connection closes on read EOF or a read error.
//
// A read frame (non-empty "read_op", see readwire.go) gets a ReadResponse.
// Any other frame is a write from an older client and gets the error Ack.
func (l *Listener) handleConn(_ context.Context, conn net.Conn) {
	defer conn.Close()
	reader := bufio.NewReader(conn)
	encoder := json.NewEncoder(conn)
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			var reply any = Ack{Status: AckError, Seq: l.seq.Add(1), Error: writesNotServed}
			if isReadFrame(line) {
				reply = l.processRead(line)
			}
			if encErr := encoder.Encode(reply); encErr != nil {
				return // client went away
			}
		}
		if err != nil {
			return // EOF or read error — done with this connection
		}
	}
}

// Close stops accepting connections, waits briefly for in-flight handlers,
// and removes the socket file. Idempotent.
func (l *Listener) Close() error {
	var err error
	l.closeOnce.Do(func() {
		err = l.ln.Close()
		// Unlink so the next owner binds cleanly, but only our own inode.
		if l.ownsSocket() {
			_ = leaseFS.Remove(l.sockPath)
		}
	})
	return err
}

// Addr returns the bound socket path (for diagnostics/tests).
func (l *Listener) Addr() string { return l.sockPath }
