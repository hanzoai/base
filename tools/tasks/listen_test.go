//go:build darwin || linux

package tasks

import (
	"context"
	"fmt"
	"net"
	"os"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/luxfi/zap"
)

// TestZAPClientListensNowhere connects the client to a Tasks server and asks
// the operating system which sockets this process listens on. The client
// submits and schedules over the connection it dialled, so it adds none. The
// server beside it adds one, which shows the probe can see a listener at all.
func TestZAPClientListensNowhere(t *testing.T) {
	before := listening(t)

	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(freePort(t)))
	srv := zap.NewNode(zap.NodeConfig{NodeID: "tasks-server", Address: addr, NoDiscovery: true})
	names := make(chan string, 2)
	serve := func(field int) zap.Handler {
		return func(_ context.Context, _ string, msg *zap.Message) (*zap.Message, error) {
			names <- msg.Root().Text(field)
			return statusReply(200)
		}
	}
	srv.Handle(OpcodeTaskSubmit, serve(fieldTaskType))
	srv.Handle(OpcodeTaskSchedule, serve(fieldName))
	if err := srv.Start(); err != nil {
		t.Fatalf("server start: %v", err)
	}
	t.Cleanup(srv.Stop)

	withServer := listening(t)
	if added := addedTo(withServer, before); len(added) != 1 {
		t.Fatalf("probe saw %v for the server's one listener", added)
	}

	c := New("", addr, nil)
	t.Cleanup(c.Stop)
	if err := c.submitZAP("settle", map[string]any{"n": 1}); err != nil {
		t.Fatalf("submit over ZAP: %v", err)
	}
	if err := c.scheduleZAP("nightly", time.Hour); err != nil {
		t.Fatalf("schedule over ZAP: %v", err)
	}
	for _, want := range []string{"settle", "nightly"} {
		if got := <-names; got != want {
			t.Fatalf("server received %q, want %q", got, want)
		}
	}

	if added := addedTo(listening(t), withServer); len(added) > 0 {
		t.Fatalf("the tasks client listens on %v", added)
	}
}

// statusReply is the reply a Tasks server sends: a status and no body.
func statusReply(status uint32) (*zap.Message, error) {
	b := zap.NewBuilder(64)
	obj := b.StartObject(respBody + 8)
	obj.SetUint32(respStatus, status)
	obj.FinishAsRoot()
	return zap.Parse(b.Finish())
}

// listening is every socket this process listens on, by descriptor, as the
// operating system reports it. A stream socket with no peer is one that
// listens: macOS does not answer SO_ACCEPTCONN, and a stream socket Go has
// finished dialling has a peer.
func listening(t *testing.T) map[int]string {
	t.Helper()
	// Names only: os.ReadDir stats each entry, and on macOS a descriptor in
	// /dev/fd can refuse fstatat, which fails the listing outright.
	dir, err := os.Open("/dev/fd")
	if err != nil {
		t.Fatalf("list descriptors: %v", err)
	}
	names, err := dir.Readdirnames(-1)
	dir.Close()
	if err != nil {
		t.Fatalf("list descriptors: %v", err)
	}
	out := make(map[int]string)
	for _, name := range names {
		fd, err := strconv.Atoi(name)
		if err != nil {
			continue
		}
		if typ, err := syscall.GetsockoptInt(fd, syscall.SOL_SOCKET, syscall.SO_TYPE); err != nil || typ != syscall.SOCK_STREAM {
			continue // not a socket, or not a stream
		}
		if _, err := syscall.Getpeername(fd); err != syscall.ENOTCONN {
			continue // connected
		}
		sa, err := syscall.Getsockname(fd)
		if err != nil {
			continue
		}
		out[fd] = sockaddr(sa)
	}
	return out
}

func sockaddr(sa syscall.Sockaddr) string {
	switch a := sa.(type) {
	case *syscall.SockaddrInet4:
		return net.JoinHostPort(net.IP(a.Addr[:]).String(), strconv.Itoa(a.Port))
	case *syscall.SockaddrInet6:
		return net.JoinHostPort(net.IP(a.Addr[:]).String(), strconv.Itoa(a.Port))
	case *syscall.SockaddrUnix:
		return a.Name
	}
	return fmt.Sprintf("%T", sa)
}

// addedTo is what now holds that before did not.
func addedTo(now, before map[int]string) []string {
	var added []string
	for fd, addr := range now {
		if was, ok := before[fd]; !ok || was != addr {
			added = append(added, addr)
		}
	}
	return added
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve free port: %v", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}
