package native

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"time"

	notificationsv1 "github.com/agynio/llm-proxy/.gen/go/agynio/api/notifications/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

const (
	subscriptionUpdatedEvent           = "subscription.updated"
	subscriptionAttachmentUpdatedEvent = "subscription_attachment.updated"
	environmentUpdatedEvent            = "environment.updated"

	// Flat rooms, not per-organization or per-environment ones. Notification
	// subscriptions are fixed at request time, and the proxy cannot enumerate
	// the organizations or environments it will serve, so it needs rooms it can
	// hold one subscription open to for the life of the process. Same shape the
	// Egress Gateway uses for egress_rules.
	SubscriptionsRoom = "llm_subscriptions"
	EnvironmentsRoom  = "environments"

	identityMetadataKey         = "x-identity-id"
	subscriberIdentityID        = "00000000-0000-0000-0000-000000000000"
	defaultNotificationsBackoff = time.Second
)

type NotificationsClient interface {
	Subscribe(context.Context, *notificationsv1.SubscribeRequest, ...grpc.CallOption) (grpc.ServerStreamingClient[notificationsv1.SubscribeResponse], error)
}

// Invalidator drops every binding resolved so far.
type Invalidator interface {
	Invalidate()
}

// InvalidationSubscriber keeps native-mode bindings honest. Without it a
// detached or rotated subscription stays live on already-established
// connections for as long as the agent CLI keeps them open -- hours, in a
// sandbox.
type InvalidationSubscriber struct {
	client  NotificationsClient
	target  Invalidator
	rooms   []string
	backoff time.Duration
}

func NewInvalidationSubscriber(client NotificationsClient, target Invalidator) *InvalidationSubscriber {
	if client == nil {
		panic("notifications client is required")
	}
	if target == nil {
		panic("invalidation target is required")
	}
	return &InvalidationSubscriber{
		client:  client,
		target:  target,
		rooms:   []string{SubscriptionsRoom, EnvironmentsRoom},
		backoff: defaultNotificationsBackoff,
	}
}

func (s *InvalidationSubscriber) Run(ctx context.Context) error {
	for ctx.Err() == nil {
		if err := s.runStream(ctx); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			log.Printf("native: invalidation stream failed: %v", err)
		}
		if !sleepContext(ctx, s.backoff) {
			return ctx.Err()
		}
	}
	return ctx.Err()
}

func (s *InvalidationSubscriber) runStream(ctx context.Context) error {
	ctx = metadata.AppendToOutgoingContext(ctx, identityMetadataKey, subscriberIdentityID)
	stream, err := s.client.Subscribe(ctx, &notificationsv1.SubscribeRequest{Rooms: s.rooms})
	if err != nil {
		return fmt.Errorf("subscribe to invalidations: %w", err)
	}
	for {
		resp, err := stream.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return fmt.Errorf("receive invalidation: %w", err)
		}
		envelope := resp.GetEnvelope()
		if envelope == nil {
			continue
		}
		switch envelope.GetEvent() {
		case subscriptionUpdatedEvent, subscriptionAttachmentUpdatedEvent, environmentUpdatedEvent:
			// Bindings live on connections rather than in a map, so there is
			// nothing to evict selectively: every one re-resolves on its next
			// request.
			s.target.Invalidate()
		}
	}
}

func sleepContext(ctx context.Context, duration time.Duration) bool {
	if duration <= 0 {
		return true
	}
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
