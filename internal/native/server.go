package native

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"

	llmv1 "github.com/agynio/llm-proxy/.gen/go/agynio/api/llm/v1"
	"github.com/agynio/llm-proxy/internal/identity"
	"google.golang.org/grpc"
)

type IdentityResolver interface {
	ResolveIdentity(ctx context.Context, sourceIdentity string) (identity.ResolvedIdentity, error)
}

type SubscriptionResolver interface {
	ResolveSubscription(ctx context.Context, req *llmv1.ResolveSubscriptionRequest, opts ...grpc.CallOption) (*llmv1.ResolveSubscriptionResponse, error)
}

// Forwarder serves one native-mode request against the vendor.
type Forwarder interface {
	Forward(w http.ResponseWriter, r *http.Request, binding Binding)
}

// Binding is everything needed to serve a connection, resolved once when the
// first request arrives and held for the connection's life.
type Binding struct {
	SubscriptionID   string
	Token            string
	AccountID        string
	UpstreamEndpoint string
	Protocol         llmv1.Protocol
	AllowedModels    []string
	OrganizationID   string
	Vendor           llmv1.Vendor
	Identity         identity.ResolvedIdentity
}

type Server struct {
	listener  *Listener
	identity  IdentityResolver
	llm       SubscriptionResolver
	certs     *LeafCertificateCache
	forwarder Forwarder

	// generation is bumped when a subscription or environment changes, which
	// invalidates every binding resolved before it. A counter rather than a
	// keyed cache: bindings live on connections, not in a map we could evict.
	mu         sync.RWMutex
	generation uint64
}

func NewServer(listener *Listener, identityResolver IdentityResolver, llm SubscriptionResolver, certs *LeafCertificateCache, forwarder Forwarder) *Server {
	if listener == nil {
		panic("listener is required")
	}
	if identityResolver == nil {
		panic("identity resolver is required")
	}
	if llm == nil {
		panic("subscription resolver is required")
	}
	if certs == nil {
		panic("leaf certificate cache is required")
	}
	if forwarder == nil {
		panic("forwarder is required")
	}
	return &Server{listener: listener, identity: identityResolver, llm: llm, certs: certs, forwarder: forwarder}
}

// Invalidate drops every binding resolved so far. Called on
// subscription.updated, subscription_attachment.updated, and
// environment.updated, so a detached credential or a tightened allowlist stops
// applying without waiting for a long-lived connection to close.
func (s *Server) Invalidate() {
	s.mu.Lock()
	s.generation++
	s.mu.Unlock()
}

func (s *Server) currentGeneration() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.generation
}

func (s *Server) Serve(ctx context.Context) error {
	go func() {
		<-ctx.Done()
		if err := s.listener.Close(); err != nil {
			log.Printf("native: close listener: %v", err)
		}
	}()
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}
		go s.handleConn(ctx, conn)
	}
}

func (s *Server) handleConn(ctx context.Context, conn *Conn) {
	defer conn.Close()

	// http/1.1 only: the SSE relay the platform path already uses is written
	// against it, and the CLI offers no ALPN of its own.
	tlsConn := tls.Server(conn, &tls.Config{
		GetCertificate: s.certificateFor(conn),
		NextProtos:     []string{"http/1.1"},
	})
	if err := tlsConn.Handshake(); err != nil {
		log.Printf("native: tls handshake on %s: %v", conn.ServiceName, err)
		return
	}
	defer tlsConn.Close()

	var (
		binding    Binding
		resolved   bool
		generation uint64
	)
	reader := bufio.NewReader(tlsConn)
	for {
		req, err := http.ReadRequest(reader)
		if err != nil {
			if !errors.Is(err, io.EOF) && ctx.Err() == nil {
				log.Printf("native: read request on %s: %v", conn.ServiceName, err)
			}
			return
		}
		req = req.WithContext(ctx)
		req.RequestURI = ""

		// Re-resolve when the binding was invalidated, so a detached credential
		// fails the next request rather than surviving the connection.
		if !resolved || generation != s.currentGeneration() {
			generation = s.currentGeneration()
			binding, err = s.resolve(ctx, conn)
			if err != nil {
				writeVendorError(tlsConn, conn.Vendor, http.StatusForbidden, err.Error())
				return
			}
			resolved = true
		}

		response := newConnResponseWriter(tlsConn)
		s.forwarder.Forward(response, req, binding)
		if err := response.finish(); err != nil {
			log.Printf("native: write response on %s: %v", conn.ServiceName, err)
			return
		}
		if response.closeAfter {
			return
		}
	}
}

func (s *Server) certificateFor(conn *Conn) func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	return func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
		host := strings.TrimSuffix(hello.ServerName, ".")
		if host == "" {
			// No SNI: the vendor host the bound service intercepts is the only
			// name this connection can legitimately be for.
			host = conn.VendorHost
		}
		// SNI selects the certificate, never the vendor. A workload that lies
		// in its ClientHello changes which certificate it is offered and
		// nothing else.
		if host != conn.VendorHost {
			return nil, fmt.Errorf("server name %q does not match %s on %s", host, conn.VendorHost, conn.ServiceName)
		}
		return s.certs.Certificate(host)
	}
}

func (s *Server) resolve(ctx context.Context, conn *Conn) (Binding, error) {
	resolvedIdentity, err := s.identity.ResolveIdentity(ctx, conn.DialerIdentityID())
	if err != nil {
		return Binding{}, fmt.Errorf("resolve caller identity: %w", err)
	}
	if resolvedIdentity.EnvironmentID == "" {
		return Binding{}, errors.New("caller identity carries no environment")
	}

	resp, err := s.llm.ResolveSubscription(ctx, &llmv1.ResolveSubscriptionRequest{
		AgentId:       resolvedIdentity.AgentID,
		EnvironmentId: resolvedIdentity.EnvironmentID,
		Vendor:        conn.Vendor,
	})
	if err != nil {
		// Refused with a platform error rather than forwarded unauthenticated,
		// which the vendor would reject opaquely.
		return Binding{}, fmt.Errorf("no subscription attached for %s: %w", vendorName(conn.Vendor), err)
	}

	return Binding{
		SubscriptionID:   resp.GetSubscriptionId(),
		Token:            resp.GetToken(),
		AccountID:        resp.GetAccountId(),
		UpstreamEndpoint: resp.GetUpstreamEndpoint(),
		Protocol:         resp.GetProtocol(),
		AllowedModels:    resp.GetAllowedModels(),
		OrganizationID:   resp.GetOrganizationId(),
		Vendor:           conn.Vendor,
		Identity:         resolvedIdentity,
	}, nil
}

func vendorName(vendor llmv1.Vendor) string {
	switch vendor {
	case llmv1.Vendor_VENDOR_CLAUDE:
		return "claude"
	case llmv1.Vendor_VENDOR_CODEX:
		return "codex"
	default:
		return "unknown"
	}
}
