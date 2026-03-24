// Package auth provides authentication and RBAC for ChronosOS.
package auth

import (
	"fmt"
	"net/http"
)

// Role defines an RBAC role.
type Role string

const (
	RoleAdmin  Role = "admin"
	RoleUser   Role = "user"
	RoleViewer Role = "viewer"
)

var roleHierarchy = map[Role]int{
	RoleAdmin:  3,
	RoleUser:   2,
	RoleViewer: 1,
}

// Service manages authentication and authorization.
type Service struct{}

func NewService() *Service { return &Service{} }

// CheckPermission verifies a user has the required role.
func (s *Service) CheckPermission(claims *UserClaims, required Role) bool {
	if claims == nil {
		return false
	}
	requiredLevel := roleHierarchy[required]
	for _, r := range claims.Roles {
		if level, ok := roleHierarchy[Role(r)]; ok && level >= requiredLevel {
			return true
		}
	}
	return false
}

// RequireRole returns middleware that checks the authenticated user has at least
// the specified role. Must be used after JWT or API key middleware.
func RequireRole(svc *Service, required Role) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := UserFromContext(r.Context())
			if !ok {
				http.Error(w, `{"error":"unauthenticated"}`, http.StatusUnauthorized)
				return
			}
			if !svc.CheckPermission(claims, required) {
				http.Error(w, fmt.Sprintf(`{"error":"insufficient permissions, requires %s"}`, required), http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
