package grpc_test

import (
	"context"
	"net"
	"testing"

	nexusv1 "github.com/thomas666-beast/nexus-payments/gen/nexus/v1"
	"github.com/thomas666-beast/nexus-payments/internal/adapter"
	"github.com/thomas666-beast/nexus-payments/internal/ledger"
	"github.com/thomas666-beast/nexus-payments/internal/service"
	"github.com/thomas666-beast/nexus-payments/internal/store"
	grpcserver "github.com/thomas666-beast/nexus-payments/internal/transport/grpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

func startServer(t *testing.T) (nexusv1.NexusClient, func()) {
	t.Helper()
	pool := newTestPool(t)
	truncate(t, pool)
	db := &store.DB{Pool: pool}
	ensureSystemAccount(t, db)
	reg := adapter.NewRegistry()
	svc := service.New(db, ledger.New(nil), reg, service.DefaultConfig(), nil)
	srv := grpcserver.NewServer(svc)

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	g := grpc.NewServer()
	nexusv1.RegisterNexusServer(g, srv)

	go func() {
		_ = g.Serve(lis)
	}()

	conn, err := grpc.Dial(lis.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		g.Stop()
		t.Fatalf("dial: %v", err)
	}

	cleanup := func() {
		_ = conn.Close()
		g.Stop()
	}
	return nexusv1.NewNexusClient(conn), cleanup
}

func TestGRPC_InitiateTransfer(t *testing.T) {
	client, cleanup := startServer(t)
	defer cleanup()

	// Get the pool that startServer used to seed accounts. We need a fresh
	// pool here so we can write fixture rows before calling the API.
	pool := newTestPool(t)
	db := &store.DB{Pool: pool}
	seedAccount(t, db, "alice", "bank-test", "USD")
	seedAccount(t, db, "bob", "bank-test", "USD")
	fundAccount(t, pool, "alice", 1000, "USD")

	ctx := metadata.AppendToOutgoingContext(context.Background(), "idempotency-key", "grpc-test-1")

	res, err := client.InitiateTransfer(ctx, &nexusv1.InitiateTransferRequest{
		FromAccount: "alice",
		ToAccount:   "bob",
		Amount:      &nexusv1.Money{AmountMinor: 100, Currency: "USD"},
	})
	if err != nil {
		t.Fatalf("InitiateTransfer: %v", err)
	}
	if res.Status != "SETTLED" {
		t.Fatalf("status want SETTLED, got %s", res.Status)
	}
	if len(res.EntryIds) != 2 {
		t.Fatalf("want 2 entry ids, got %d", len(res.EntryIds))
	}

	// Replay with the same idempotency key.
	res2, err := client.InitiateTransfer(ctx, &nexusv1.InitiateTransferRequest{
		FromAccount: "alice",
		ToAccount:   "bob",
		Amount:      &nexusv1.Money{AmountMinor: 100, Currency: "USD"},
	})
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if res2.TransferId != res.TransferId {
		t.Fatalf("replay returned different transfer id: %s vs %s", res2.TransferId, res.TransferId)
	}
}

func TestGRPC_GetTransferStatus(t *testing.T) {
	client, cleanup := startServer(t)
	defer cleanup()

	pool := newTestPool(t)
	db := &store.DB{Pool: pool}
	seedAccount(t, db, "alice", "bank-test", "USD")
	seedAccount(t, db, "bob", "bank-test", "USD")
	fundAccount(t, pool, "alice", 1000, "USD")

	res, err := client.InitiateTransfer(context.Background(), &nexusv1.InitiateTransferRequest{
		FromAccount: "alice",
		ToAccount:   "bob",
		Amount:      &nexusv1.Money{AmountMinor: 100, Currency: "USD"},
	})
	if err != nil {
		t.Fatalf("InitiateTransfer: %v", err)
	}

	status, err := client.GetTransferStatus(context.Background(), &nexusv1.GetTransferStatusRequest{
		TransferId: res.TransferId,
	})
	if err != nil {
		t.Fatalf("GetTransferStatus: %v", err)
	}
	if status.Status != "SETTLED" {
		t.Fatalf("status want SETTLED, got %s", status.Status)
	}
}

func TestGRPC_GetBalance(t *testing.T) {
	client, cleanup := startServer(t)
	defer cleanup()

	pool := newTestPool(t)
	db := &store.DB{Pool: pool}
	seedAccount(t, db, "alice", "bank-test", "USD")
	fundAccount(t, pool, "alice", 1000, "USD")

	bal, err := client.GetBalance(context.Background(), &nexusv1.GetBalanceRequest{
		AccountId: "alice",
	})
	if err != nil {
		t.Fatalf("GetBalance: %v", err)
	}
	if bal.Balance.AmountMinor != 1000 {
		t.Fatalf("balance want 1000, got %d", bal.Balance.AmountMinor)
	}
}
