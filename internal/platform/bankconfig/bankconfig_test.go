package bankconfig_test

import (
	"testing"

	"github.com/thomas666-beast/nexus-payments/internal/platform/bankconfig"
)

func TestParseEnv_Empty(t *testing.T) {
	t.Setenv("NEXUS_BANKS", "")
	banks, err := bankconfig.ParseEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(banks) != 0 {
		t.Fatalf("want 0 banks, got %d", len(banks))
	}
}

func TestParseEnv_TwoBanks(t *testing.T) {
	t.Setenv("NEXUS_BANKS", "bank-a=bank-a:7071,bank-b=bank-b:7071")
	banks, err := bankconfig.ParseEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(banks) != 2 {
		t.Fatalf("want 2 banks, got %d", len(banks))
	}
	if banks[0].ID != "bank-a" || banks[0].Addr != "bank-a:7071" {
		t.Fatalf("unexpected bank[0]: %+v", banks[0])
	}
	if banks[1].ID != "bank-b" || banks[1].Addr != "bank-b:7071" {
		t.Fatalf("unexpected bank[1]: %+v", banks[1])
	}
}

func TestParseEnv_Malformed(t *testing.T) {
	t.Setenv("NEXUS_BANKS", "bank-a-no-equals")
	_, err := bankconfig.ParseEnv()
	if err == nil {
		t.Fatal("expected error for malformed entry")
	}
}

func TestParseEnv_Duplicate(t *testing.T) {
	t.Setenv("NEXUS_BANKS", "bank-a=foo:1,bank-a=bar:2")
	_, err := bankconfig.ParseEnv()
	if err == nil {
		t.Fatal("expected error for duplicate id")
	}
}
