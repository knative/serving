package helpers

import "context"

type (
	revCtxKey struct{}
)

type revCtx struct {
	anns map[string]string
}

// WithAnnotations attaches the Revision annotation to the context.
func WithAnnotations(ctx context.Context, anns map[string]string) context.Context {
	return context.WithValue(ctx, revCtxKey{}, &revCtx{
		anns: anns,
	})
}

// AnnotationsFrom retrieves the Revision annotations array from the context.
func AnnotationsFrom(ctx context.Context) map[string]string {
	if ctx.Value(revCtxKey{}) != nil {
		return ctx.Value(revCtxKey{}).(*revCtx).anns
	}
	return nil
}
