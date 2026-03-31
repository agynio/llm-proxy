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

type dialerSourceConn struct {
	stubConn
	dialerID string
	sourceID string
}

func (conn dialerSourceConn) GetDialerIdentityId() string { return conn.dialerID }
func (conn dialerSourceConn) SourceIdentifier() string    { return conn.sourceID }

type dialerConn struct {
	stubConn
	dialerID string
}

func (conn dialerConn) GetDialerIdentityId() string { return conn.dialerID }

type sourceConn struct {
	stubConn
	sourceID string
}

func (conn sourceConn) SourceIdentifier() string { return conn.sourceID }

func TestSourceIdentityFromConnPrefersDialerID(t *testing.T) {
	conn := dialerSourceConn{dialerID: "dialer-id", sourceID: "source-id"}
	assertSourceIdentity(t, conn, "dialer-id", true)
}

func TestSourceIdentityFromConnFallsBackToSourceIdentifier(t *testing.T) {
	conn := dialerSourceConn{dialerID: "  ", sourceID: "source-id"}
	assertSourceIdentity(t, conn, "source-id", true)
}

func TestSourceIdentityFromConnSourceOnly(t *testing.T) {
	conn := sourceConn{sourceID: "source-id"}
	assertSourceIdentity(t, conn, "source-id", true)
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
