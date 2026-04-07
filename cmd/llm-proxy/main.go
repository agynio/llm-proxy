package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
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
	"github.com/agynio/llm-proxy/internal/zitimanager"
	"github.com/agynio/llm-proxy/internal/zitimgmtclient"
	"github.com/openziti/sdk-golang/ziti"
	"google.golang.org/grpc"
)

const shutdownTimeout = 10 * time.Second

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

	go func() {
		log.Printf("llm-proxy listening on %s", cfg.ListenAddress)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("http server stopped: %w", err)
		}
	}()

	if cfg.ZitiEnabled {
		var listenerMu sync.Mutex
		var currentListener net.Listener
		listenerFactory := func(zitiCtx ziti.Context) (net.Listener, error) {
			return zitiCtx.ListenWithOptions("llm-proxy", ziti.DefaultListenOptions())
		}
		onNewListener := func(listener net.Listener) {
			listenerMu.Lock()
			previousListener := currentListener
			currentListener = listener
			listenerMu.Unlock()

			log.Printf("llm-proxy listening on ziti service llm-proxy")
			go func(activeListener net.Listener) {
				if err := server.Serve(activeListener); err != nil && !errors.Is(err, http.ErrServerClosed) && !errors.Is(err, net.ErrClosed) {
					errCh <- fmt.Errorf("ziti server stopped: %w", err)
				}
			}(listener)

			if previousListener != nil {
				if err := previousListener.Close(); err != nil {
					log.Printf("failed to close previous ziti listener: %v", err)
				}
			}
		}

		zitiManager, err := zitimanager.New(
			ctx,
			zitiMgmtClient,
			zitimgmtv1.ServiceType_SERVICE_TYPE_LLM_PROXY,
			cfg.ZitiLeaseRenewalInterval,
			cfg.ZitiEnrollmentTimeout,
			listenerFactory,
			onNewListener,
		)
		if err != nil {
			return fmt.Errorf("setup ziti manager: %w", err)
		}
		defer zitiManager.Close()

		go zitiManager.RunLeaseRenewal(ctx)
	}

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
