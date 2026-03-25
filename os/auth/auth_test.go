package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCheckPermission_AdminCanDoEverything(t *testing.T) {
	svc := NewService()
	claims := &UserClaims{UserID: "u1", Roles: []string{"admin"}}
	for _, role := range []Role{RoleAdmin, RoleUser, RoleViewer} {
		if !svc.CheckPermission(claims, role) {
			t.Errorf("admin should have %s permission", role)
		}
	}
}

func TestCheckPermission_UserCannotAdmin(t *testing.T) {
	svc := NewService()
	claims := &UserClaims{UserID: "u1", Roles: []string{"user"}}
	if svc.CheckPermission(claims, RoleAdmin) {
		t.Error("user should not have admin permission")
	}
	if !svc.CheckPermission(claims, RoleUser) {
		t.Error("user should have user permission")
	}
	if !svc.CheckPermission(claims, RoleViewer) {
		t.Error("user should have viewer permission")
	}
}

func TestCheckPermission_ViewerRestricted(t *testing.T) {
	svc := NewService()
	claims := &UserClaims{UserID: "u1", Roles: []string{"viewer"}}
	if svc.CheckPermission(claims, RoleAdmin) {
		t.Error("viewer should not have admin permission")
	}
	if svc.CheckPermission(claims, RoleUser) {
		t.Error("viewer should not have user permission")
	}
	if !svc.CheckPermission(claims, RoleViewer) {
		t.Error("viewer should have viewer permission")
	}
}

func TestCheckPermission_NilClaims(t *testing.T) {
	svc := NewService()
	if svc.CheckPermission(nil, RoleViewer) {
		t.Error("nil claims should not pass")
	}
}

func TestRequireRole_Authorized(t *testing.T) {
	svc := NewService()
	handler := RequireRole(svc, RoleUser)(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest("GET", "/test", http.NoBody)
	ctx := WithUser(req.Context(), &UserClaims{UserID: "u1", Roles: []string{"admin"}})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("got status %d, want 200", rec.Code)
	}
}

func TestRequireRole_Forbidden(t *testing.T) {
	svc := NewService()
	handler := RequireRole(svc, RoleAdmin)(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest("GET", "/test", http.NoBody)
	ctx := WithUser(req.Context(), &UserClaims{UserID: "u1", Roles: []string{"viewer"}})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("got status %d, want 403", rec.Code)
	}
}

func TestRequireRole_NoAuth(t *testing.T) {
	svc := NewService()
	handler := RequireRole(svc, RoleViewer)(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest("GET", "/test", http.NoBody)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("got status %d, want 401", rec.Code)
	}
}
