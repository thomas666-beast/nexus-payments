package grpc

import (
	"context"
	"errors"

	nexusv1 "github.com/thomas666-beast/nexus-payments/gen/nexus/v1"
	"github.com/thomas666-beast/nexus-payments/internal/domain"
	"github.com/thomas666-beast/nexus-payments/internal/ledger"
	"github.com/thomas666-beast/nexus-payments/internal/service"
	"github.com/thomas666-beast/nexus-payments/internal/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const idempotencyKeyHeader = "idempotency-key"

type Server struct {
	nexusv1.UnimplementedNexusServer
	svc *service.Service
}

func NewServer(svc *service.Service) *Server {
	return &Server{svc: svc}
}

func (s *Server) InitiateTransfer(ctx context.Context, req *nexusv1.InitiateTransferRequest) (*nexusv1.InitiateTransferResponse, error) {
	if req.Amount == nil {
		return nil, status.Error(codes.InvalidArgument, "amount required")
	}
	idemKey := ""
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if vals := md.Get(idempotencyKeyHeader); len(vals) > 0 {
			idemKey = vals[0]
		}
	}

	res, err := s.svc.Execute(ctx, service.ExecuteRequest{
		FromAccount:    domain.AccountID(req.FromAccount),
		ToAccount:      domain.AccountID(req.ToAccount),
		Amount:         domain.Money{Amount: req.Amount.AmountMinor, Currency: domain.Currency(req.Amount.Currency)},
		Reason:         req.Reason,
		IdempotencyKey: idemKey,
	})
	if err != nil {
		return nil, mapError(err)
	}

	entryIDs := make([]string, 0, len(res.Entries))
	for _, e := range res.Entries {
		entryIDs = append(entryIDs, string(e.ID))
	}

	return &nexusv1.InitiateTransferResponse{
		TransferId:  string(res.Transfer.ID),
		Status:      string(res.Transfer.Status),
		Amount:      &nexusv1.Money{AmountMinor: res.Transfer.Amount.Amount, Currency: string(res.Transfer.Amount.Currency)},
		FromAccount: string(res.Transfer.FromAccount),
		ToAccount:   string(res.Transfer.ToAccount),
		EntryIds:    entryIDs,
	}, nil
}

func (s *Server) GetTransferStatus(ctx context.Context, req *nexusv1.GetTransferStatusRequest) (*nexusv1.GetTransferStatusResponse, error) {
	t, err := s.svc.GetTransfer(ctx, domain.TransferID(req.TransferId))
	if err != nil {
		return nil, mapError(err)
	}
	return &nexusv1.GetTransferStatusResponse{
		TransferId:  string(t.ID),
		Status:      string(t.Status),
		Reason:      t.Reason,
		Amount:      &nexusv1.Money{AmountMinor: t.Amount.Amount, Currency: string(t.Amount.Currency)},
		FromAccount: string(t.FromAccount),
		ToAccount:   string(t.ToAccount),
	}, nil
}

func (s *Server) GetBalance(ctx context.Context, req *nexusv1.GetBalanceRequest) (*nexusv1.GetBalanceResponse, error) {
	bal, err := s.svc.GetBalance(ctx, domain.AccountID(req.AccountId))
	if err != nil {
		return nil, mapError(err)
	}
	return &nexusv1.GetBalanceResponse{
		Balance: &nexusv1.Money{AmountMinor: bal.Amount, Currency: string(bal.Currency)},
	}, nil
}

// mapError translates service errors into gRPC status codes.
func mapError(err error) error {
	switch {
	case errors.Is(err, service.ErrInvalidRequest):
		return status.Error(codes.InvalidArgument, err.Error())
	case errors.Is(err, service.ErrIdempotencyConflict):
		return status.Error(codes.AlreadyExists, err.Error())
	case errors.Is(err, service.ErrIdempotencyInProgress):
		return status.Error(codes.Aborted, err.Error())
	case errors.Is(err, store.ErrNotFound):
		return status.Error(codes.NotFound, err.Error())
	case errors.Is(err, ledger.ErrInsufficientFunds):
		return status.Error(codes.FailedPrecondition, err.Error())
	case errors.Is(err, ledger.ErrAccountNotActive):
		return status.Error(codes.FailedPrecondition, err.Error())
	default:
		return status.Error(codes.Internal, err.Error())
	}
}
