package ingest

import (
	"context"
	"errors"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// ErrUnknownToken means no host owns the presented token.
var ErrUnknownToken = errors.New("unknown token")

// Identity is who a token belongs to. It comes from the server's own records, never from the client.
type Identity struct {
	HostID string
	OrgID  string
}

// TokenVerifier maps a bearer token to the one host (and organization) it belongs to.
type TokenVerifier interface {
	IdentityForToken(ctx context.Context, token string) (Identity, error)
}

type identityKey struct{}

// IdentityFrom returns the authenticated identity of this stream; the zero value means none.
func IdentityFrom(ctx context.Context) Identity {
	id, _ := ctx.Value(identityKey{}).(Identity)
	return id
}

// HostFrom returns the authenticated host of this stream, or "" if none.
func HostFrom(ctx context.Context) string { return IdentityFrom(ctx).HostID }

type authedStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s *authedStream) Context() context.Context { return s.ctx }

// AuthInterceptor requires "authorization: Bearer <token>" on every stream and records
// which host the token belongs to. It checks once per stream, not per message.
func AuthInterceptor(v TokenVerifier) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		md, _ := metadata.FromIncomingContext(ss.Context())
		var token string
		if vals := md.Get("authorization"); len(vals) > 0 {
			token = strings.TrimSpace(strings.TrimPrefix(vals[0], "Bearer "))
		}
		if token == "" {
			return status.Error(codes.Unauthenticated, "missing token")
		}

		id, err := v.IdentityForToken(ss.Context(), token)
		switch {
		case errors.Is(err, ErrUnknownToken):
			return status.Error(codes.Unauthenticated, "invalid token")
		case err != nil:
			return status.Error(codes.Unavailable, "auth backend unavailable")
		}

		return handler(srv, &authedStream{ServerStream: ss, ctx: context.WithValue(ss.Context(), identityKey{}, id)})
	}
}
