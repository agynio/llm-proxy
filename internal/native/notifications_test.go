package native

import (
	"context"
	"testing"
	"time"

	notificationsv1 "github.com/agynio/llm-proxy/.gen/go/agynio/api/notifications/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type failingNotifications struct{ attempts int }

func (f *failingNotifications) Subscribe(context.Context, *notificationsv1.SubscribeRequest, ...grpc.CallOption) (grpc.ServerStreamingClient[notificationsv1.SubscribeResponse], error) {
	f.attempts++
	return nil, status.Error(codes.PermissionDenied, "permission denied")
}

type countingInvalidator struct{ count int }

func (c *countingInvalidator) Invalidate() { c.count++ }

// A stream failing for a reason retrying cannot fix -- an unregistered room, a
// revoked identity -- must not retry at a fixed interval forever. Without the
// backoff this logs once a second for the life of the process.
func TestInvalidationSubscriberBacksOffOnRepeatedFailure(t *testing.T) {
	client := &failingNotifications{}
	subscriber := NewInvalidationSubscriber(client, &countingInvalidator{})
	subscriber.backoff = time.Millisecond

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer cancel()
	_ = subscriber.Run(ctx)

	// Fixed 1ms retries over 60ms would be dozens of attempts; doubling from
	// 1ms reaches 64ms in seven, so the window admits only a handful.
	if client.attempts == 0 {
		t.Fatal("subscriber never attempted to subscribe")
	}
	if client.attempts > 10 {
		t.Fatalf("subscriber retried %d times in 60ms; backoff is not growing", client.attempts)
	}
}
