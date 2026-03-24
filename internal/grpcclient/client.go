package grpcclient

import (
	"fmt"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Client wraps a gRPC connection with a typed service client.
type Client[T any] struct {
	conn   *grpc.ClientConn
	client T
}

func New[T any](target string, factory func(grpc.ClientConnInterface) T) (*Client[T], error) {
	if strings.TrimSpace(target) == "" {
		return nil, fmt.Errorf("target is required")
	}
	if factory == nil {
		return nil, fmt.Errorf("client factory is required")
	}

	conn, err := grpc.NewClient(
		target,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithChainUnaryInterceptor(identityUnaryClientInterceptor()),
		grpc.WithChainStreamInterceptor(identityStreamClientInterceptor()),
	)
	if err != nil {
		return nil, err
	}

	return &Client[T]{
		conn:   conn,
		client: factory(conn),
	}, nil
}

func (c *Client[T]) Close() error {
	return c.conn.Close()
}

func (c *Client[T]) Service() T {
	return c.client
}
