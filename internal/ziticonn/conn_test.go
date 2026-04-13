package ziticonn

import (
	"net"
	"testing"
	"time"
)

type stubConn struct{}

func (stubConn) Read(_ []byte) (int, error)         { return 0, nil }
func (stubConn) Write(_ []byte) (int, error)        { return 0, nil }
func (stubConn) Close() error                       { return nil }
func (stubConn) LocalAddr() net.Addr                { return stubAddr{} }
func (stubConn) RemoteAddr() net.Addr               { return stubAddr{} }
func (stubConn) SetDeadline(_ time.Time) error      { return nil }
func (stubConn) SetReadDeadline(_ time.Time) error  { return nil }
func (stubConn) SetWriteDeadline(_ time.Time) error { return nil }

type stubAddr struct{}

func (stubAddr) Network() string { return "stub" }
func (stubAddr) String() string  { return "stub" }

type dialerConn struct {
	stubConn
	dialerID string
}

func (conn dialerConn) GetDialerIdentityId() string { return conn.dialerID }

func TestSourceIdentityFromConnDialerID(t *testing.T) {
	conn := dialerConn{dialerID: "dialer-id"}
	assertSourceIdentity(t, conn, "dialer-id", true)
}

func TestSourceIdentityFromConnMissingIdentity(t *testing.T) {
	conn := stubConn{}
	assertSourceIdentity(t, conn, "", false)
}

func TestSourceIdentityFromConnDialerOnlyEmptyFallsThrough(t *testing.T) {
	conn := dialerConn{dialerID: ""}
	assertSourceIdentity(t, conn, "", false)
}

func assertSourceIdentity(t *testing.T, conn net.Conn, want string, wantOK bool) {
	t.Helper()

	identity, ok := SourceIdentityFromConn(conn)
	if ok != wantOK {
		t.Fatalf("expected ok=%t, got %t", wantOK, ok)
	}
	if identity != want {
		t.Fatalf("expected identity %q, got %q", want, identity)
	}
}
