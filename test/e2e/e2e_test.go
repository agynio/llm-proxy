//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"testing"
	"time"

	authorizationv1 "github.com/agynio/llm-proxy/.gen/go/agynio/api/authorization/v1"
	llmv1 "github.com/agynio/llm-proxy/.gen/go/agynio/api/llm/v1"
	usersv1 "github.com/agynio/llm-proxy/.gen/go/agynio/api/users/v1"
	zitimgmtv1 "github.com/agynio/llm-proxy/.gen/go/agynio/api/ziti_management/v1"
	"github.com/agynio/llm-proxy/internal/apitokenresolver"
	"github.com/agynio/llm-proxy/internal/auth"
	"github.com/agynio/llm-proxy/internal/proxy"
	"github.com/agynio/llm-proxy/internal/zitimgmtclient"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const providerToken = "provider-token"

var (
	fakeLLMServer   *FakeLLMServer
	fakeUsersServer *FakeUsersServer
	fakeAuthzServer *FakeAuthzServer
	fakeZitiServer  *FakeZitiMgmtServer
	fakeProvider    *FakeProviderServer
	proxyBaseURL    string
	proxyHandler    http.Handler
)

func TestMain(m *testing.M) {
	cleanup := make([]func(), 0, 12)
	fail := func(err error) {
		for i := len(cleanup) - 1; i >= 0; i-- {
			cleanup[i]()
		}
		_, _ = fmt.Fprintf(os.Stderr, "e2e setup failed: %v\n", err)
		os.Exit(1)
	}

	fakeLLMServer = NewFakeLLMServer()
	llmAddr, llmStop, err := startGRPCServer(func(server *grpc.Server) {
		llmv1.RegisterLLMServiceServer(server, fakeLLMServer)
	})
	if err != nil {
		fail(err)
	}
	cleanup = append(cleanup, llmStop)

	fakeUsersServer = NewFakeUsersServer()
	usersAddr, usersStop, err := startGRPCServer(func(server *grpc.Server) {
		usersv1.RegisterUsersServiceServer(server, fakeUsersServer)
	})
	if err != nil {
		fail(err)
	}
	cleanup = append(cleanup, usersStop)

	fakeAuthzServer = NewFakeAuthzServer()
	authzAddr, authzStop, err := startGRPCServer(func(server *grpc.Server) {
		authorizationv1.RegisterAuthorizationServiceServer(server, fakeAuthzServer)
	})
	if err != nil {
		fail(err)
	}
	cleanup = append(cleanup, authzStop)

	fakeZitiServer = NewFakeZitiMgmtServer()
	zitiAddr, zitiStop, err := startGRPCServer(func(server *grpc.Server) {
		zitimgmtv1.RegisterZitiManagementServiceServer(server, fakeZitiServer)
	})
	if err != nil {
		fail(err)
	}
	cleanup = append(cleanup, zitiStop)

	fakeProvider = NewFakeProviderServer()
	cleanup = append(cleanup, fakeProvider.Close)

	llmConn, err := grpc.NewClient(llmAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		fail(err)
	}
	cleanup = append(cleanup, func() { _ = llmConn.Close() })
	llmClient := llmv1.NewLLMServiceClient(llmConn)

	authzConn, err := grpc.NewClient(authzAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		fail(err)
	}
	cleanup = append(cleanup, func() { _ = authzConn.Close() })
	authzClient := authorizationv1.NewAuthorizationServiceClient(authzConn)

	usersConn, err := grpc.NewClient(usersAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		fail(err)
	}
	cleanup = append(cleanup, func() { _ = usersConn.Close() })
	usersClient := usersv1.NewUsersServiceClient(usersConn)

	zitiMgmtClient, err := zitimgmtclient.NewClient(zitiAddr)
	if err != nil {
		fail(err)
	}
	cleanup = append(cleanup, func() { _ = zitiMgmtClient.Close() })

	apiTokenResolver := apitokenresolver.NewResolver(usersClient)
	proxyHandler = auth.Middleware(zitiMgmtClient, apiTokenResolver)(proxy.NewHandler(llmClient, authzClient, &http.Client{}))

	proxyAddr, proxyStop, err := startHTTPServer(proxyHandler)
	if err != nil {
		fail(err)
	}
	cleanup = append(cleanup, proxyStop)
	proxyBaseURL = "http://" + proxyAddr

	exitCode := m.Run()
	for i := len(cleanup) - 1; i >= 0; i-- {
		cleanup[i]()
	}
	os.Exit(exitCode)
}

func startGRPCServer(register func(*grpc.Server)) (string, func(), error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", nil, err
	}
	server := grpc.NewServer()
	register(server)
	go func() {
		_ = server.Serve(listener)
	}()
	stop := func() {
		server.GracefulStop()
		_ = listener.Close()
	}
	return listener.Addr().String(), stop, nil
}

func startHTTPServer(handler http.Handler) (string, func(), error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", nil, err
	}
	server := &http.Server{Handler: handler}
	go func() {
		_ = server.Serve(listener)
	}()
	stop := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
		_ = listener.Close()
	}
	return listener.Addr().String(), stop, nil
}

func setupTest(t *testing.T) {
	t.Helper()
	resetFakes()
	t.Cleanup(resetFakes)
}

func resetFakes() {
	fakeLLMServer.Reset()
	fakeUsersServer.Reset()
	fakeAuthzServer.Reset()
	fakeZitiServer.Reset()
	fakeProvider.Reset()
}

func registerModel(modelID string, remoteName string, organizationID string) {
	fakeLLMServer.RegisterModel(modelID, &llmv1.ResolveModelResponse{
		Endpoint:       fakeProvider.URL(),
		Token:          providerToken,
		RemoteName:     remoteName,
		OrganizationId: organizationID,
	})
}

func allowIdentity(identityID string, organizationID string) {
	fakeAuthzServer.SetCheck("identity:"+identityID, "member", "organization:"+organizationID, true)
}
