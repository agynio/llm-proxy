package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	authorizationv1 "github.com/agynio/llm-proxy/.gen/go/agynio/api/authorization/v1"
	llmv1 "github.com/agynio/llm-proxy/.gen/go/agynio/api/llm/v1"
	"github.com/agynio/llm-proxy/internal/identity"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var (
	ErrInvalidBody     = errors.New("invalid body")
	ErrMissingModel    = errors.New("model is required")
	ErrMissingIdentity = errors.New("identity is required")
	ErrForbidden       = errors.New("access denied")
)

const maxRequestBodySize int64 = 1 << 20

type ModelResolver interface {
	ResolveModel(ctx context.Context, req *llmv1.ResolveModelRequest, opts ...grpc.CallOption) (*llmv1.ResolveModelResponse, error)
}

type AuthorizationChecker interface {
	Check(ctx context.Context, req *authorizationv1.CheckRequest, opts ...grpc.CallOption) (*authorizationv1.CheckResponse, error)
}

type Handler struct {
	llmClient   ModelResolver
	authzClient AuthorizationChecker
	client      *http.Client
}

func NewHandler(llmClient ModelResolver, authzClient AuthorizationChecker, client *http.Client) http.Handler {
	if llmClient == nil {
		panic("llm client is required")
	}
	if authzClient == nil {
		panic("authorization client is required")
	}
	if client == nil {
		panic("http client is required")
	}
	return &Handler{llmClient: llmClient, authzClient: authzClient, client: client}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/v1/responses" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	resolvedIdentity, ok := identity.IdentityFromContext(r.Context())
	if !ok {
		writeProxyError(w, ErrMissingIdentity)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodySize)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}
	log.Printf("proxy: request body read method=%s path=%s size=%d", r.Method, r.URL.Path, len(body))

	payload, modelID, stream, err := parseRequestPayload(body)
	if err != nil {
		rawModel := extractRawModelValue(body)
		log.Printf("proxy: parse request failed err=%v raw_model=%s", err, rawModel)
		writeProxyError(w, err)
		return
	}
	log.Printf("proxy: parsed request model_id=%s stream=%t", modelID, stream)

	resolvedModel, err := h.llmClient.ResolveModel(r.Context(), &llmv1.ResolveModelRequest{ModelId: modelID})
	if err != nil {
		writeProxyError(w, err)
		return
	}

	endpoint := strings.TrimRight(strings.TrimSpace(resolvedModel.GetEndpoint()), "/")
	remoteName := strings.TrimSpace(resolvedModel.GetRemoteName())
	log.Printf("proxy: resolved model remote_name=%s endpoint=%s", remoteName, endpoint)
	if endpoint == "" {
		writeProxyError(w, errors.New("provider endpoint is required"))
		return
	}
	if remoteName == "" {
		writeProxyError(w, errors.New("remote model name is required"))
		return
	}
	token := strings.TrimSpace(resolvedModel.GetToken())
	if token == "" {
		writeProxyError(w, errors.New("provider token is required"))
		return
	}
	organizationID := strings.TrimSpace(resolvedModel.GetOrganizationId())
	if organizationID == "" {
		writeProxyError(w, errors.New("organization id is required"))
		return
	}

	if err := h.authorizeRequest(r.Context(), resolvedIdentity, organizationID); err != nil {
		writeProxyError(w, err)
		return
	}

	updatedBody, err := updateRequestPayload(payload, remoteName, stream)
	if err != nil {
		writeProxyError(w, err)
		return
	}

	providerReq, err := buildProviderRequest(r.Context(), endpoint, token, updatedBody, stream)
	if err != nil {
		writeProxyError(w, err)
		return
	}

	if stream {
		h.streamResponse(w, r, providerReq)
		return
	}

	h.forwardResponse(w, providerReq)
}

func (h *Handler) forwardResponse(w http.ResponseWriter, req *http.Request) {
	resp, err := h.client.Do(req)
	if err != nil {
		writeProxyError(w, fmt.Errorf("send request: %w", err))
		return
	}
	log.Printf("proxy: upstream response status=%d", resp.StatusCode)
	defer closeResponseBody(resp.Body)

	copyHeaders(w.Header(), resp.Header, nil)
	w.WriteHeader(resp.StatusCode)
	if _, err := io.Copy(w, resp.Body); err != nil {
		log.Printf("proxy: forward response failed: %v", err)
	}
}

func (h *Handler) streamResponse(w http.ResponseWriter, r *http.Request, req *http.Request) {
	resp, err := h.client.Do(req)
	if err != nil {
		writeProxyError(w, fmt.Errorf("send request: %w", err))
		return
	}
	log.Printf("proxy: upstream response status=%d", resp.StatusCode)
	defer closeResponseBody(resp.Body)

	copyHeaders(w.Header(), resp.Header, map[string]struct{}{"Content-Length": {}})
	w.WriteHeader(resp.StatusCode)
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		_, _ = io.Copy(w, resp.Body)
		return
	}
	if err := streamToClient(r.Context(), w, resp.Body); err != nil {
		log.Printf("proxy: stream response failed: %v", err)
	}
}

func (h *Handler) authorizeRequest(ctx context.Context, resolved identity.ResolvedIdentity, organizationID string) error {
	user := fmt.Sprintf("identity:%s", resolved.IdentityID)
	object := fmt.Sprintf("organization:%s", organizationID)

	resp, err := h.authzClient.Check(ctx, &authorizationv1.CheckRequest{
		TupleKey: &authorizationv1.TupleKey{
			User:     user,
			Relation: "member",
			Object:   object,
		},
	})
	if err != nil {
		return err
	}
	if !resp.GetAllowed() {
		return ErrForbidden
	}
	return nil
}

func buildProviderRequest(ctx context.Context, endpoint string, token string, body []byte, stream bool) (*http.Request, error) {
	url := endpoint
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if stream {
		req.Header.Set("Accept", "text/event-stream")
	}
	req.Header.Set("Authorization", "Bearer "+token)

	return req, nil
}

func extractRawModelValue(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil {
		return ""
	}
	rawModel, ok := payload["model"]
	if !ok {
		return ""
	}
	rawModel = bytes.TrimSpace(rawModel)
	if len(rawModel) == 0 {
		return ""
	}
	var modelStr string
	if err := json.Unmarshal(rawModel, &modelStr); err == nil {
		return strings.TrimSpace(modelStr)
	}
	return string(rawModel)
}

func parseRequestPayload(body []byte) (map[string]any, string, bool, error) {
	if len(body) == 0 {
		return nil, "", false, fmt.Errorf("%w: body is empty", ErrInvalidBody)
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, "", false, fmt.Errorf("%w: %v", ErrInvalidBody, err)
	}

	rawModel, ok := payload["model"]
	if !ok {
		return payload, "", false, ErrMissingModel
	}
	modelStr, ok := rawModel.(string)
	if !ok || strings.TrimSpace(modelStr) == "" {
		return payload, "", false, fmt.Errorf("%w: model must be a string", ErrInvalidBody)
	}
	modelID, err := uuid.Parse(modelStr)
	if err != nil {
		return payload, "", false, fmt.Errorf("%w: model must be a UUID", ErrInvalidBody)
	}

	stream := false
	if rawStream, ok := payload["stream"]; ok {
		value, ok := rawStream.(bool)
		if !ok {
			return payload, "", false, fmt.Errorf("%w: stream must be a boolean", ErrInvalidBody)
		}
		stream = value
	}

	return payload, modelID.String(), stream, nil
}

func updateRequestPayload(payload map[string]any, remoteName string, forceStream bool) ([]byte, error) {
	payload["model"] = remoteName
	if forceStream {
		payload["stream"] = true
	}
	updated, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidBody, err)
	}
	return updated, nil
}

func writeProxyError(w http.ResponseWriter, err error) {
	statusCode := http.StatusBadGateway
	if grpcStatus, ok := status.FromError(err); ok {
		statusCode = grpcStatusToHTTP(grpcStatus.Code())
	} else {
		switch {
		case errors.Is(err, ErrInvalidBody), errors.Is(err, ErrMissingModel):
			statusCode = http.StatusBadRequest
		case errors.Is(err, ErrMissingIdentity):
			statusCode = http.StatusUnauthorized
		case errors.Is(err, ErrForbidden):
			statusCode = http.StatusForbidden
		}
	}

	log.Printf("proxy: error status=%d err=%v", statusCode, err)

	message := err.Error()
	if statusCode >= http.StatusInternalServerError {
		message = http.StatusText(statusCode)
		if message == "" {
			message = "server error"
		}
	}

	http.Error(w, message, statusCode)
}

func grpcStatusToHTTP(code codes.Code) int {
	switch code {
	case codes.InvalidArgument, codes.OutOfRange:
		return http.StatusBadRequest
	case codes.NotFound:
		return http.StatusNotFound
	case codes.AlreadyExists, codes.Aborted:
		return http.StatusConflict
	case codes.Unauthenticated:
		return http.StatusUnauthorized
	case codes.PermissionDenied:
		return http.StatusForbidden
	case codes.FailedPrecondition:
		return http.StatusPreconditionFailed
	case codes.ResourceExhausted:
		return http.StatusTooManyRequests
	case codes.Unimplemented:
		return http.StatusNotImplemented
	case codes.Unavailable:
		return http.StatusServiceUnavailable
	case codes.DeadlineExceeded:
		return http.StatusGatewayTimeout
	default:
		return http.StatusBadGateway
	}
}

func copyHeaders(dst, src http.Header, skip map[string]struct{}) {
	for key, values := range src {
		canonical := http.CanonicalHeaderKey(key)
		if skip != nil {
			if _, ok := skip[canonical]; ok {
				continue
			}
		}
		for _, value := range values {
			dst.Add(canonical, value)
		}
	}
}

func closeResponseBody(body io.Closer) {
	if err := body.Close(); err != nil {
		log.Printf("proxy: response body close failed: %v", err)
	}
}

func streamToClient(ctx context.Context, w http.ResponseWriter, body io.Reader) error {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return errors.New("streaming unsupported")
	}
	buffer := make([]byte, 32*1024)
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		n, err := body.Read(buffer)
		if n > 0 {
			if _, writeErr := w.Write(buffer[:n]); writeErr != nil {
				return writeErr
			}
			flusher.Flush()
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
	}
}
