package grpcadapter

import (
	"context"
	"time"

	bankv1 "github.com/thomas666-beast/nexus-payments/gen/bank/v1"
	"github.com/thomas666-beast/nexus-payments/internal/adapter"
	"github.com/thomas666-beast/nexus-payments/internal/domain"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Client implements adapter.Adapter against a remote virtual bank over gRPC.
type Client struct {
	bankID string
	conn   *grpc.ClientConn
	client bankv1.BankClient
}

func Dial(bankID, addr string) (*Client, error) {
	conn, err := grpc.Dial(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, err
	}
	return &Client{
		bankID: bankID,
		conn:   conn,
		client: bankv1.NewBankClient(conn),
	}, nil
}

func (c *Client) Close() error { return c.conn.Close() }

func (c *Client) BankID() string { return c.bankID }

func (c *Client) Reserve(ctx context.Context, req adapter.ReserveRequest) (adapter.Reservation, error) {
	resp, err := c.client.Reserve(ctx, &bankv1.ReserveRequest{
		TransferId:    string(req.TransferID),
		AccountId:     string(req.AccountID),
		Amount:        &bankv1.Money{AmountMinor: req.Amount.Amount, Currency: string(req.Amount.Currency)},
		ExpiresAtUnix: req.ExpiresAt.Unix(),
	})
	if err != nil {
		return adapter.Reservation{}, err
	}
	return adapter.Reservation{
		ID:         resp.ReservationId,
		BankID:     resp.BankId,
		TransferID: req.TransferID,
		AccountID:  req.AccountID,
		Amount:     req.Amount,
		ExpiresAt:  req.ExpiresAt,
	}, nil
}

func (c *Client) Commit(ctx context.Context, res adapter.Reservation) error {
	_, err := c.client.Commit(ctx, &bankv1.CommitRequest{ReservationId: res.ID})
	return err
}

func (c *Client) Rollback(ctx context.Context, res adapter.Reservation) error {
	_, err := c.client.Rollback(ctx, &bankv1.RollbackRequest{ReservationId: res.ID})
	return err
}

func (c *Client) Reconcile(ctx context.Context, from, to time.Time) ([]domain.TransferID, error) {
	resp, err := c.client.Reconcile(ctx, &bankv1.ReconcileRequest{
		FromUnix: from.Unix(),
		ToUnix:   to.Unix(),
	})
	if err != nil {
		return nil, err
	}
	ids := make([]domain.TransferID, 0, len(resp.TransferIds))
	for _, id := range resp.TransferIds {
		ids = append(ids, domain.TransferID(id))
	}
	return ids, nil
}
