package model

import "context"

type Client struct {
	provider Provider
}

func NewClient(provider Provider) *Client {
	return &Client{provider: provider}
}

func (client *Client) Reply(ctx context.Context, request Request) (Response, error) {
	return client.provider.Generate(ctx, request)
}
