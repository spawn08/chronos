package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"strings"
	"testing"
)

func TestValidateJWT_InvalidPartCount(t *testing.T) {
	_, err := validateJWT("only.two", "secret")
	if err == nil || !strings.Contains(err.Error(), "invalid token format") {
		t.Fatalf("expected format error, got %v", err)
	}
}

func TestValidateJWT_InvalidPayloadEncoding(t *testing.T) {
	token := "aa.bb!!!.cc"
	_, err := validateJWT(token, "secret")
	if err == nil || !strings.Contains(err.Error(), "payload") {
		t.Fatalf("expected payload encoding error, got %v", err)
	}
}

func TestValidateJWT_InvalidClaimsJSON(t *testing.T) {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(`not json`))
	sigInput := header + "." + payload
	mac := hmac.New(sha256.New, []byte("secret"))
	mac.Write([]byte(sigInput))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	token := header + "." + payload + "." + sig
	_, err := validateJWT(token, "secret")
	if err == nil || !strings.Contains(err.Error(), "invalid claims") {
		t.Fatalf("expected claims unmarshal error, got %v", err)
	}
}

func TestValidateJWT_SignatureMismatch(t *testing.T) {
	claims := UserClaims{UserID: "u1"}
	tok := CreateTestToken(claims, "correct-secret")
	// Break signature segment
	parts := strings.Split(tok, ".")
	parts[2] = "wrongsig"
	badTok := strings.Join(parts, ".")
	_, err := validateJWT(badTok, "correct-secret")
	if err == nil || !strings.Contains(err.Error(), "signature") {
		t.Fatalf("expected signature error, got %v", err)
	}
}
