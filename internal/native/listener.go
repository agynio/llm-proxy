package native

import (
	"log"
	"net"
	"strings"
	"sync"
	"time"

	llmv1 "github.com/agynio/llm-proxy/.gen/go/agynio/api/llm/v1"
	"github.com/openziti/edge-api/rest_model"
	"github.com/openziti/sdk-golang/ziti"
	"github.com/openziti/sdk-golang/ziti/edge"
)

// InterceptServiceRole is the role attribute every vendor intercept service
// carries, so the proxy binds them by role rather than by name.
const InterceptServiceRole = "llm-intercept-services"

const defaultRoleReconcileInterval = 15 * time.Second

// vendorServices maps each intercept service to the vendor it carries and the
// hostname the CLI addressed. The service name is the vendor: it is a fact
// about which OpenZiti service the platform's own policies routed the
// connection to, not a value the client sends.
var vendorServices = map[string]struct {
	vendor llmv1.Vendor
	host   string
}{
	"llm-intercept-anthropic": {llmv1.Vendor_VENDOR_ANTHROPIC, "api.anthropic.com"},
	"llm-intercept-openai":    {llmv1.Vendor_VENDOR_OPENAI, "chatgpt.com"},
}

// VendorForService reports which vendor a bound service carries.
func VendorForService(serviceName string) (llmv1.Vendor, string, bool) {
	entry, ok := vendorServices[serviceName]
	if !ok {
		return llmv1.Vendor_VENDOR_UNSPECIFIED, "", false
	}
	return entry.vendor, entry.host, true
}

type ZitiContext interface {
	RefreshServices() error
	GetServices() ([]rest_model.ServiceDetail, error)
	ListenWithOptions(serviceName string, options *ziti.ListenOptions) (edge.Listener, error)
}

// Conn carries the vendor its listener bound alongside the dialer's identity.
type Conn struct {
	net.Conn
	Vendor      llmv1.Vendor
	VendorHost  string
	ServiceName string
	dialer      string
}

// DialerIdentityID is the workload's OpenZiti identity, which is what native
// mode authenticates by -- there is no caller credential to check.
func (c *Conn) DialerIdentityID() string { return c.dialer }

// Listener accepts on every vendor intercept service the proxy may bind,
// reconciling the set as services appear. One listener per service, so the
// vendor is known before the TLS handshake begins.
type Listener struct {
	ctx       ZitiContext
	interval  time.Duration
	acceptCh  chan *Conn
	closeCh   chan struct{}
	once      sync.Once
	mu        sync.Mutex
	listeners map[string]edge.Listener
}

func NewListener(ctx ZitiContext) *Listener {
	return NewListenerWithInterval(ctx, defaultRoleReconcileInterval)
}

func NewListenerWithInterval(ctx ZitiContext, interval time.Duration) *Listener {
	if ctx == nil {
		panic("ziti context is required")
	}
	if interval <= 0 {
		panic("reconcile interval must be positive")
	}
	l := &Listener{
		ctx:       ctx,
		interval:  interval,
		acceptCh:  make(chan *Conn),
		closeCh:   make(chan struct{}),
		listeners: map[string]edge.Listener{},
	}
	go l.run()
	return l
}

func (l *Listener) Accept() (*Conn, error) {
	select {
	case conn := <-l.acceptCh:
		return conn, nil
	case <-l.closeCh:
		return nil, net.ErrClosed
	}
}

func (l *Listener) Close() error {
	l.once.Do(func() {
		close(l.closeCh)
		l.mu.Lock()
		listeners := l.listeners
		l.listeners = map[string]edge.Listener{}
		l.mu.Unlock()
		for _, listener := range listeners {
			_ = listener.Close()
		}
	})
	return nil
}

func (l *Listener) run() {
	l.reconcile()
	ticker := time.NewTicker(l.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			l.reconcile()
		case <-l.closeCh:
			return
		}
	}
}

func (l *Listener) reconcile() {
	if err := l.ctx.RefreshServices(); err != nil {
		log.Printf("native: refresh ziti services: %v", err)
		return
	}
	services, err := l.ctx.GetServices()
	if err != nil {
		log.Printf("native: list ziti services: %v", err)
		return
	}
	for _, service := range services {
		if service.Name == nil || !hasRole(service.RoleAttributes, InterceptServiceRole) {
			continue
		}
		// A service carrying the role but not in the vendor table is one this
		// build does not know about; binding it would accept traffic nothing
		// can resolve a credential for.
		if _, _, ok := VendorForService(*service.Name); !ok {
			log.Printf("native: ignoring unknown intercept service %q", *service.Name)
			continue
		}
		l.ensureListener(*service.Name)
	}
}

func (l *Listener) ensureListener(serviceName string) {
	l.mu.Lock()
	_, exists := l.listeners[serviceName]
	l.mu.Unlock()
	if exists {
		return
	}
	// Bind non-addressed: workload intercepts dial without dialOptions.identity,
	// and an identity-addressed terminator never matches an empty instanceId.
	listener, err := l.ctx.ListenWithOptions(serviceName, &ziti.ListenOptions{})
	if err != nil {
		log.Printf("native: listen for %q: %v", serviceName, err)
		return
	}
	l.mu.Lock()
	if existing := l.listeners[serviceName]; existing != nil {
		l.mu.Unlock()
		_ = listener.Close()
		return
	}
	l.listeners[serviceName] = listener
	l.mu.Unlock()
	log.Printf("native: bound vendor intercept service %s", serviceName)
	go l.accept(serviceName, listener)
}

func (l *Listener) accept(serviceName string, listener edge.Listener) {
	vendor, host, _ := VendorForService(serviceName)
	for {
		conn, err := listener.AcceptEdge()
		if err != nil {
			if !l.isClosed() {
				log.Printf("native: listener %q stopped: %v", serviceName, err)
			}
			_ = listener.Close()
			l.removeListener(serviceName, listener)
			return
		}
		if err := conn.CompleteAcceptSuccess(); err != nil {
			log.Printf("native: complete accept on %q: %v", serviceName, err)
			conn.Close()
			continue
		}
		wrapped := &Conn{
			Conn:        conn,
			Vendor:      vendor,
			VendorHost:  host,
			ServiceName: serviceName,
			dialer:      conn.GetDialerIdentityId(),
		}
		select {
		case l.acceptCh <- wrapped:
		case <-l.closeCh:
			_ = wrapped.Close()
			return
		}
	}
}

func (l *Listener) removeListener(serviceName string, listener edge.Listener) {
	l.mu.Lock()
	if l.listeners[serviceName] == listener {
		delete(l.listeners, serviceName)
	}
	l.mu.Unlock()
}

func (l *Listener) isClosed() bool {
	select {
	case <-l.closeCh:
		return true
	default:
		return false
	}
}

func hasRole(attributes *rest_model.Attributes, role string) bool {
	if attributes == nil {
		return false
	}
	for _, attribute := range *attributes {
		if strings.EqualFold(attribute, role) {
			return true
		}
	}
	return false
}
