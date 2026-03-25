//go:build e2e

package e2e

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sync"

	authorizationv1 "github.com/agynio/llm-proxy/.gen/go/agynio/api/authorization/v1"
	identityv1 "github.com/agynio/llm-proxy/.gen/go/agynio/api/identity/v1"
	llmv1 "github.com/agynio/llm-proxy/.gen/go/agynio/api/llm/v1"
	usersv1 "github.com/agynio/llm-proxy/.gen/go/agynio/api/users/v1"
	zitimgmtv1 "github.com/agynio/llm-proxy/.gen/go/agynio/api/ziti_management/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type FakeLLMServer struct {
	llmv1.UnimplementedLLMServiceServer
	mu     sync.Mutex
	models map[string]*llmv1.ResolveModelResponse
}

func NewFakeLLMServer() *FakeLLMServer {
	server := &FakeLLMServer{}
	server.Reset()
	return server
}

func (f *FakeLLMServer) Reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.models = make(map[string]*llmv1.ResolveModelResponse)
}

func (f *FakeLLMServer) RegisterModel(modelID string, resp *llmv1.ResolveModelResponse) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.models[modelID] = resp
}

func (f *FakeLLMServer) ResolveModel(_ context.Context, req *llmv1.ResolveModelRequest) (*llmv1.ResolveModelResponse, error) {
	f.mu.Lock()
	resp, ok := f.models[req.GetModelId()]
	f.mu.Unlock()
	if !ok {
		return nil, status.Error(codes.NotFound, "model not found")
	}
	return resp, nil
}

type FakeUsersServer struct {
	usersv1.UnimplementedUsersServiceServer
	mu     sync.Mutex
	tokens map[string]*usersv1.ResolveAPITokenResponse
}

func NewFakeUsersServer() *FakeUsersServer {
	server := &FakeUsersServer{}
	server.Reset()
	return server
}

func (f *FakeUsersServer) Reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tokens = make(map[string]*usersv1.ResolveAPITokenResponse)
}

func (f *FakeUsersServer) RegisterToken(rawToken string, identityID string) string {
	hash := sha256.Sum256([]byte(rawToken))
	tokenHash := hex.EncodeToString(hash[:])
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tokens[tokenHash] = &usersv1.ResolveAPITokenResponse{IdentityId: identityID}
	return tokenHash
}

func (f *FakeUsersServer) ResolveAPIToken(_ context.Context, req *usersv1.ResolveAPITokenRequest) (*usersv1.ResolveAPITokenResponse, error) {
	f.mu.Lock()
	resp, ok := f.tokens[req.GetTokenHash()]
	f.mu.Unlock()
	if !ok {
		return nil, status.Error(codes.NotFound, "api token not found")
	}
	return resp, nil
}

type FakeAuthzServer struct {
	authorizationv1.UnimplementedAuthorizationServiceServer
	mu           sync.Mutex
	checks       map[string]bool
	defaultAllow bool
}

func NewFakeAuthzServer() *FakeAuthzServer {
	server := &FakeAuthzServer{}
	server.Reset()
	return server
}

func (f *FakeAuthzServer) Reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.checks = make(map[string]bool)
	f.defaultAllow = false
}

func (f *FakeAuthzServer) SetDefaultAllow(allowed bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.defaultAllow = allowed
}

func (f *FakeAuthzServer) SetCheck(user string, relation string, object string, allowed bool) {
	key := authzKey(user, relation, object)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.checks[key] = allowed
}

func (f *FakeAuthzServer) Check(_ context.Context, req *authorizationv1.CheckRequest) (*authorizationv1.CheckResponse, error) {
	if req == nil || req.GetTupleKey() == nil {
		return nil, status.Error(codes.InvalidArgument, "tuple key is required")
	}
	key := authzKey(req.GetTupleKey().GetUser(), req.GetTupleKey().GetRelation(), req.GetTupleKey().GetObject())
	f.mu.Lock()
	allowed, ok := f.checks[key]
	defaultAllow := f.defaultAllow
	f.mu.Unlock()
	if !ok {
		allowed = defaultAllow
	}
	return &authorizationv1.CheckResponse{Allowed: allowed}, nil
}

type FakeZitiMgmtServer struct {
	zitimgmtv1.UnimplementedZitiManagementServiceServer
	mu         sync.Mutex
	identities map[string]*zitimgmtv1.ResolveIdentityResponse
}

func NewFakeZitiMgmtServer() *FakeZitiMgmtServer {
	server := &FakeZitiMgmtServer{}
	server.Reset()
	return server
}

func (f *FakeZitiMgmtServer) Reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.identities = make(map[string]*zitimgmtv1.ResolveIdentityResponse)
}

func (f *FakeZitiMgmtServer) RegisterIdentity(zitiID string, identityID string, identityType identityv1.IdentityType) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.identities[zitiID] = &zitimgmtv1.ResolveIdentityResponse{
		IdentityId:   identityID,
		IdentityType: identityType,
	}
}

func (f *FakeZitiMgmtServer) ResolveIdentity(_ context.Context, req *zitimgmtv1.ResolveIdentityRequest) (*zitimgmtv1.ResolveIdentityResponse, error) {
	f.mu.Lock()
	resp, ok := f.identities[req.GetZitiIdentityId()]
	f.mu.Unlock()
	if !ok {
		return nil, status.Error(codes.NotFound, "identity not found")
	}
	return resp, nil
}

func authzKey(user string, relation string, object string) string {
	return user + "|" + relation + "|" + object
}
