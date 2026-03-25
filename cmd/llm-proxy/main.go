package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	authorizationv1 "github.com/agynio/llm-proxy/.gen/go/agynio/api/authorization/v1"
	llmv1 "github.com/agynio/llm-proxy/.gen/go/agynio/api/llm/v1"
	usersv1 "github.com/agynio/llm-proxy/.gen/go/agynio/api/users/v1"
	zitimgmtv1 "github.com/agynio/llm-proxy/.gen/go/agynio/api/ziti_management/v1"
	"github.com/agynio/llm-proxy/internal/apitokenresolver"
	"github.com/agynio/llm-proxy/internal/auth"
	"github.com/agynio/llm-proxy/internal/config"
	"github.com/agynio/llm-proxy/internal/grpcclient"
	"github.com/agynio/llm-proxy/internal/proxy"
	"github.com/agynio/llm-proxy/internal/ziticonn"
	"github.com/agynio/llm-proxy/internal/zitimgmtclient"
	"github.com/openziti/sdk-golang/ziti"
	"google.golang.org/grpc"
)

const (
	shutdownTimeout     = 10 * time.Second
	retryInitialBackoff = 1 * time.Second
	retryMaxBackoff     = 30 * time.Second
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("llm-proxy: %v", err)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := config.LoadConfigFromEnv()
	if err != nil {
		return err
	}

	cleanup := make([]func(), 0, 4)
	defer func() {
		for _, closeFn := range cleanup {
			closeFn()
		}
	}()

	llmClient := mustClient(cfg.LLMServiceAddress, "llm", llmv1.NewLLMServiceClient, &cleanup)
	authzClient := mustClient(cfg.AuthorizationServiceAddress, "authorization", authorizationv1.NewAuthorizationServiceClient, &cleanup)
	usersClient := mustClient(cfg.UsersServiceAddress, "users", usersv1.NewUsersServiceClient, &cleanup)

	apiTokenResolver := apitokenresolver.NewResolver(usersClient)

	var zitiMgmtClient *zitimgmtclient.Client
	if cfg.ZitiEnabled {
		zitiMgmtClient, err = zitimgmtclient.NewClient(cfg.ZitiManagementAddress)
		if err != nil {
			return fmt.Errorf("create ziti management client: %w", err)
		}
		defer func() {
			if closeErr := zitiMgmtClient.Close(); closeErr != nil {
				log.Printf("failed to close ziti management client: %v", closeErr)
			}
		}()
	}

	var zitiResolver auth.IdentityResolver
	if zitiMgmtClient != nil {
		zitiResolver = zitiMgmtClient
	}

	proxyHandler := proxy.NewHandler(llmClient, authzClient, &http.Client{})
	handler := auth.Middleware(zitiResolver, apiTokenResolver)(proxyHandler)

	connContext := func(ctx context.Context, conn net.Conn) context.Context {
		sourceIdentity, ok := ziticonn.SourceIdentityFromConn(conn)
		if !ok {
			return ctx
		}
		return ziticonn.WithSourceIdentity(ctx, sourceIdentity)
	}

	server := &http.Server{
		Addr:        cfg.ListenAddress,
		Handler:     handler,
		ConnContext: connContext,
	}

	errCh := make(chan error, 2)

	if cfg.ZitiEnabled {
		enrollmentCtx, cancel := context.WithTimeout(ctx, cfg.ZitiEnrollmentTimeout)
		defer cancel()

		var zitiIdentityID string
		var identityJSON []byte
		if err := retryWithBackoff(enrollmentCtx, func(attemptCtx context.Context) error {
			var requestErr error
			zitiIdentityID, identityJSON, requestErr = zitiMgmtClient.RequestServiceIdentity(attemptCtx, zitimgmtv1.ServiceType_SERVICE_TYPE_LLM_PROXY)
			return requestErr
		}); err != nil {
			return fmt.Errorf("request ziti service identity: %w", err)
		}

		zitiConfig := &ziti.Config{}
		if err := json.Unmarshal(identityJSON, zitiConfig); err != nil {
			return fmt.Errorf("parse ziti identity: %w", err)
		}

		zitiContext, err := ziti.NewContext(zitiConfig)
		if err != nil {
			return fmt.Errorf("create ziti context: %w", err)
		}
		defer zitiContext.Close()

		go renewLease(ctx, zitiMgmtClient, zitiIdentityID, cfg.ZitiLeaseRenewalInterval)

		listener, err := zitiContext.ListenWithOptions("llm-proxy", ziti.DefaultListenOptions())
		if err != nil {
			return fmt.Errorf("listen on ziti service: %w", err)
		}

		log.Printf("llm-proxy listening on ziti service llm-proxy")
		go func() {
			if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
				errCh <- fmt.Errorf("ziti server stopped: %w", err)
			}
		}()
	}

	go func() {
		log.Printf("llm-proxy listening on %s", cfg.ListenAddress)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("http server stopped: %w", err)
		}
	}()

	select {
	case <-ctx.Done():
	case err := <-errCh:
		return err
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown http: %w", err)
	}

	return nil
}

func renewLease(ctx context.Context, client *zitimgmtclient.Client, identityID string, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if ctx.Err() != nil {
				return
			}
			if err := client.ExtendIdentityLease(ctx, identityID); err != nil {
				log.Printf("failed to extend ziti lease: %v", err)
			}
		}
	}
}

func retryWithBackoff(ctx context.Context, fn func(context.Context) error) error {
	backoff := retryInitialBackoff
	attempt := 1
	for {
		err := fn(ctx)
		if err == nil {
			return nil
		}

		if ctx.Err() != nil {
			return ctx.Err()
		}

		delay := backoff
		if delay > retryMaxBackoff {
			delay = retryMaxBackoff
		}
		if deadline, ok := ctx.Deadline(); ok {
			remaining := time.Until(deadline)
			if remaining <= 0 {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				return context.DeadlineExceeded
			}
			if delay > remaining {
				delay = remaining
			}
		}

		log.Printf("ziti enrollment failed (attempt %d), retrying in %s: %v", attempt, delay, err)

		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}

		if backoff < retryMaxBackoff {
			backoff *= 2
			if backoff > retryMaxBackoff {
				backoff = retryMaxBackoff
			}
		}
		attempt++
	}
}

func mustClient[T any](target, name string, factory func(grpc.ClientConnInterface) T, cleanup *[]func()) T {
	client, err := grpcclient.New(target, factory)
	if err != nil {
		log.Fatalf("failed to create %s gRPC client: %v", name, err)
	}

	if cleanup != nil {
		*cleanup = append(*cleanup, func() {
			if err := client.Close(); err != nil {
				log.Printf("failed to close %s gRPC client: %v", name, err)
			}
		})
	}

	return client.Service()
}
