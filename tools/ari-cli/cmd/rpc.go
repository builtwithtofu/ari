package cmd

import (
	"context"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/client"
)

func callDaemonRPC[Response any](ctx context.Context, socketPath, method string, request any) (Response, error) {
	var response Response
	if err := client.New(socketPath).Call(ctx, method, request, &response); err != nil {
		return response, err
	}
	return response, nil
}
