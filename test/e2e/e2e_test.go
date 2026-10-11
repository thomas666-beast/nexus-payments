//go:build e2e

package e2e_test

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"

	bankv1 "github.com/thomas666-beast/nexus-payments/gen/bank/v1"
	nexusv1 "github.com/thomas666-beast/nexus-payments/gen/nexus/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	nexusAddr = "localhost:9090"
	bankAAddr = "localhost:7071"
	bankBAddr = "localhost:7072"
)

func dialNexus(t *testing.T) nexusv1.NexusClient {
	t.Helper()
	conn, err := grpc.Dial(nexusAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial nexus: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return nexusv1.NewNexusClient(conn)
}

func dialBank(t *testing.T, addr string) bankv1.BankClient {
	t.Helper()
	conn, err := grpc.Dial(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial bank %s: %v", addr, err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return bankv1.NewBankClient(conn)
}

// seedNexusAccounts inserts the accounts and genesis balance directly into
// nexus-postgres. Runs via docker exec so we don't need an admin API yet.
func seedNexusAccounts(t *testing.T) {
	t.Helper()
	sql := `
		INSERT INTO accounts (id, bank_id, external_ref, currency, holder, status) VALUES
			('system-genesis', 'system', 'system-genesis', 'USD', 'System', 'ACTIVE'),
			('e2e-alice', 'bank-a', 'e2e-alice', 'USD', 'Alice', 'ACTIVE'),
			('e2e-bob',   'bank-b', 'e2e-bob',   'USD', 'Bob',   'ACTIVE'),
			('e2e-down-alice', 'bank-a', 'e2e-down-alice', 'USD', 'Alice', 'ACTIVE'),
			('e2e-down-bob',   'bank-b', 'e2e-down-bob',   'USD', 'Bob',   'ACTIVE')
		ON CONFLICT (id) DO NOTHING;

		INSERT INTO transfers (id, from_account, to_account, amount_minor, currency, status, reason)
		VALUES ('genesis-e2e-alice', 'system-genesis', 'e2e-alice', 100000, 'USD', 'SETTLED', 'genesis')
		ON CONFLICT (id) DO NOTHING;

		INSERT INTO ledger_entries (id, transfer_id, account_id, direction, amount_minor, currency)
		VALUES ('genesis-entry-e2e-alice', 'genesis-e2e-alice', 'e2e-alice', 'CREDIT', 100000, 'USD')
		ON CONFLICT (id) DO NOTHING;

		INSERT INTO transfers (id, from_account, to_account, amount_minor, currency, status, reason)
		VALUES ('genesis-e2e-down-alice', 'system-genesis', 'e2e-down-alice', 100000, 'USD', 'SETTLED', 'genesis')
		ON CONFLICT (id) DO NOTHING;

		INSERT INTO ledger_entries (id, transfer_id, account_id, direction, amount_minor, currency)
		VALUES ('genesis-entry-e2e-down-alice', 'genesis-e2e-down-alice', 'e2e-down-alice', 'CREDIT', 100000, 'USD')
		ON CONFLICT (id) DO NOTHING;
	`
	cmd := exec.Command("docker", "exec", "-i", "nexus-postgres",
		"psql", "-U", "nexus", "-d", "nexus", "-v", "ON_ERROR_STOP=1")
	cmd.Stdin = strings.NewReader(sql)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("seed nexus: %v\noutput:\n%s", err, out)
	}
}

func TestE2E_HappyPath(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	seedNexusAccounts(t)

	bankA := dialBank(t, bankAAddr)
	bankB := dialBank(t, bankBAddr)
	nexus := dialNexus(t)

	if _, err := bankA.CreateAccount(ctx, &bankv1.CreateAccountRequest{
		AccountId:      "e2e-alice",
		OpeningBalance: &bankv1.Money{AmountMinor: 100000, Currency: "USD"},
	}); err != nil {
		t.Fatalf("bank-a create alice: %v", err)
	}
	if _, err := bankB.CreateAccount(ctx, &bankv1.CreateAccountRequest{
		AccountId:      "e2e-bob",
		OpeningBalance: &bankv1.Money{AmountMinor: 0, Currency: "USD"},
	}); err != nil {
		t.Fatalf("bank-b create bob: %v", err)
	}

	balBefore, err := bankA.GetBalance(ctx, &bankv1.GetBalanceRequest{AccountId: "e2e-alice"})
	if err != nil {
		t.Fatalf("bank-a balance: %v", err)
	}
	if balBefore.Balance.AmountMinor != 100000 {
		t.Fatalf("alice starting balance want 100000, got %d", balBefore.Balance.AmountMinor)
	}

	res, err := nexus.InitiateTransfer(ctx, &nexusv1.InitiateTransferRequest{
		FromAccount: "e2e-alice",
		ToAccount:   "e2e-bob",
		Amount:      &nexusv1.Money{AmountMinor: 250, Currency: "USD"},
	})
	if err != nil {
		t.Fatalf("InitiateTransfer: %v", err)
	}
	if res.Status != "SETTLED" {
		t.Fatalf("status want SETTLED, got %s", res.Status)
	}

	balAfter, err := bankA.GetBalance(ctx, &bankv1.GetBalanceRequest{AccountId: "e2e-alice"})
	if err != nil {
		t.Fatalf("bank-a balance after: %v", err)
	}
	if balAfter.Balance.AmountMinor != 99750 {
		t.Fatalf("alice balance after want 99750, got %d", balAfter.Balance.AmountMinor)
	}

	status, err := nexus.GetTransferStatus(ctx, &nexusv1.GetTransferStatusRequest{
		TransferId: res.TransferId,
	})
	if err != nil {
		t.Fatalf("GetTransferStatus: %v", err)
	}
	if status.Status != "SETTLED" {
		t.Fatalf("nexus status want SETTLED, got %s", status.Status)
	}

	t.Logf("transfer %s settled: bank-a alice = %d",
		res.TransferId, balAfter.Balance.AmountMinor)
}

func TestE2E_BankDown_CircuitOpens(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	seedNexusAccounts(t)

	bankA := dialBank(t, bankAAddr)
	bankB := dialBank(t, bankBAddr)
	nexus := dialNexus(t)

	if _, err := bankA.CreateAccount(ctx, &bankv1.CreateAccountRequest{
		AccountId:      "e2e-down-alice",
		OpeningBalance: &bankv1.Money{AmountMinor: 100000, Currency: "USD"},
	}); err != nil {
		t.Fatalf("create alice: %v", err)
	}
	if _, err := bankB.CreateAccount(ctx, &bankv1.CreateAccountRequest{
		AccountId:      "e2e-down-bob",
		OpeningBalance: &bankv1.Money{AmountMinor: 0, Currency: "USD"},
	}); err != nil {
		t.Fatalf("create bob: %v", err)
	}

	stopBank(t, "bank-a")
	defer startBank(t, "bank-a")

	time.Sleep(2 * time.Second)

	var fastFails int
	for i := 0; i < 10; i++ {
		start := time.Now()
		_, err := nexus.InitiateTransfer(ctx, &nexusv1.InitiateTransferRequest{
			FromAccount: "e2e-down-alice",
			ToAccount:   "e2e-down-bob",
			Amount:      &nexusv1.Money{AmountMinor: 100, Currency: "USD"},
		})
		elapsed := time.Since(start)
		if err != nil {
			fastFails++
			t.Logf("fail %d: %v (%.2fs)", i, err, elapsed.Seconds())
		} else {
			t.Logf("call %d unexpectedly succeeded", i)
		}
	}

	if fastFails == 0 {
		t.Fatal("expected at least one failure with bank-a down")
	}

	t.Logf("fast fails with bank-a down: %d/10", fastFails)
}

func stopBank(t *testing.T, name string) {
	t.Helper()
	cmd := exec.Command("docker", "stop", name)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("docker stop %s: %v: %s", name, err, out)
	}
}

func startBank(t *testing.T, name string) {
	t.Helper()
	cmd := exec.Command("docker", "start", name)
	if err := cmd.Run(); err != nil {
		t.Logf("docker start %s: %v", name, err)
	}
}
