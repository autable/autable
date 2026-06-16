package auth

import "testing"

func TestPasswordUserNormalizesEmailAndChecksPassword(t *testing.T) {
	user, err := NewPasswordUser(PasswordRegistration{
		Email:    " Person@Example.COM ",
		Password: "correct horse",
	})
	if err != nil {
		t.Fatal(err)
	}

	if user.Email != "person@example.com" {
		t.Fatalf("unexpected email: %q", user.Email)
	}
	if !user.CheckPassword("correct horse") {
		t.Fatal("expected password to match")
	}
	if user.CheckPassword("wrong horse") {
		t.Fatal("unexpected password match")
	}
}

func TestOIDCUserUsesEmailFallback(t *testing.T) {
	passwordUser, err := NewPasswordUser(PasswordRegistration{
		Email:    "person@example.com",
		Password: "correct horse",
	})
	if err != nil {
		t.Fatal(err)
	}
	oidcUser, err := NewOIDCUser(OIDCIdentity{
		ProviderName: "main",
		Subject:      "sub-123",
		Email:        "PERSON@example.com",
	})
	if err != nil {
		t.Fatal(err)
	}

	if !SameLoginFallback(passwordUser, oidcUser) {
		t.Fatal("expected users to match by normalized email fallback")
	}
}
