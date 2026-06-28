package netguard

import (
	"context"
	"errors"
	"testing"
)

func TestResolvePublicAddressRejectsLoopbackBeforeDial(t *testing.T) {
	_, err := ResolvePublicAddress(context.Background(), "127.0.0.1:80")
	if !errors.Is(err, ErrBlockedTarget) {
		t.Fatalf("ResolvePublicAddress error = %v, want ErrBlockedTarget", err)
	}
}

func TestRejectBlockedHostAllowsOrdinaryHostnames(t *testing.T) {
	if err := RejectBlockedHost("vendor.example"); err != nil {
		t.Fatalf("RejectBlockedHost returned error for ordinary hostname: %v", err)
	}
}
