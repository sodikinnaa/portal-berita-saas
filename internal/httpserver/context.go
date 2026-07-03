package httpserver

import (
	"context"

	"porta-berita/internal/cms"
)

type contextKey string

const userContextKey contextKey = "user"
const apiPrincipalContextKey contextKey = "api_principal"

func withUser(ctx context.Context, user *cms.User) context.Context {
	return context.WithValue(ctx, userContextKey, user)
}

func userFromRequest(r interface{ Context() context.Context }) *cms.User {
	user, _ := r.Context().Value(userContextKey).(*cms.User)
	return user
}

func withAPIPrincipal(ctx context.Context, principal *cms.APIPrincipal) context.Context {
	return context.WithValue(ctx, apiPrincipalContextKey, principal)
}

func apiPrincipalFromRequest(r interface{ Context() context.Context }) *cms.APIPrincipal {
	principal, _ := r.Context().Value(apiPrincipalContextKey).(*cms.APIPrincipal)
	return principal
}
