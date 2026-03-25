package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

const testSecret = "test-secret-key-for-jwt"

func TestJWTMiddleware_ValidToken(t *testing.T) {
	claims := UserClaims{
		UserID: "user-123",
		Roles:  []string{"admin"},
		Exp:    time.Now().Add(time.Hour).Unix(),
	}
	token := CreateTestToken(claims, testSecret)

	handler := JWTMiddleware(JWTConfig{Secret: testSecret})(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u, ok := UserFromContext(r.Context())
			if !ok {
				t.Error("user not in context")
				return
			}
			if u.UserID != "user-123" {
				t.Errorf("got user_id=%q, want user-123", u.UserID)
			}
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest("GET", "/api/test", http.NoBody)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("got status %d, want 200", rec.Code)
	}
}

func TestJWTMiddleware_MissingHeader(t *testing.T) {
	handler := JWTMiddleware(JWTConfig{Secret: testSecret})(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest("GET", "/api/test", http.NoBody)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("got status %d, want 401", rec.Code)
	}
}

func TestJWTMiddleware_InvalidToken(t *testing.T) {
	handler := JWTMiddleware(JWTConfig{Secret: testSecret})(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest("GET", "/api/test", http.NoBody)
	req.Header.Set("Authorization", "Bearer invalid.token.here")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("got status %d, want 401", rec.Code)
	}
}

func TestJWTMiddleware_ExpiredToken(t *testing.T) {
	claims := UserClaims{
		UserID: "user-123",
		Exp:    time.Now().Add(-time.Hour).Unix(),
	}
	token := CreateTestToken(claims, testSecret)

	handler := JWTMiddleware(JWTConfig{Secret: testSecret})(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest("GET", "/api/test", http.NoBody)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("got status %d, want 401", rec.Code)
	}
}

func TestJWTMiddleware_SkipPaths(t *testing.T) {
	handler := JWTMiddleware(JWTConfig{
		Secret:    testSecret,
		SkipPaths: []string{"/health"},
	})(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest("GET", "/health", http.NoBody)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("health endpoint should skip auth, got status %d", rec.Code)
	}
}

func TestJWTMiddleware_WrongSecret(t *testing.T) {
	claims := UserClaims{UserID: "user-123", Exp: time.Now().Add(time.Hour).Unix()}
	token := CreateTestToken(claims, "wrong-secret")

	handler := JWTMiddleware(JWTConfig{Secret: testSecret})(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest("GET", "/api/test", http.NoBody)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("got status %d, want 401", rec.Code)
	}
}

func TestCreateTestToken_Roundtrip(t *testing.T) {
	claims := UserClaims{UserID: "test", Roles: []string{"admin", "user"}, Exp: time.Now().Add(time.Hour).Unix()}
	token := CreateTestToken(claims, testSecret)
	decoded, err := validateJWT(token, testSecret)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if decoded.UserID != "test" {
		t.Errorf("got user_id=%q, want test", decoded.UserID)
	}
	if len(decoded.Roles) != 2 {
		t.Errorf("got %d roles, want 2", len(decoded.Roles))
	}
}
