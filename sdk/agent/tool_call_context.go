package agent

import "context"

type toolCallIDKey struct{}

// ToolCallIDFromContext returns the provider's tool-call identity for the
// currently executing handler. It is host-bound and not taken from tool args.
func ToolCallIDFromContext(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(toolCallIDKey{}).(string)
	return id, ok && id != ""
}

func withToolCallID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, toolCallIDKey{}, id)
}
