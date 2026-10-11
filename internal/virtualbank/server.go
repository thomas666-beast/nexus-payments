package virtualbank

import (
	"context"
	"errors"
	"time"

	bankv1 "github.com/thomas666-beast/nexus-payments/gen/bank/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Server implements bankv1.BankServer.
type Server struct {
	bankv1.UnimplementedBankServer
	svc      *Service
	bankID   string
	faults   *FaultConfig
}

func NewServer(svc *Service, bankID string, faults *FaultConfig) *Server {
	return &Server{svc: svc, bankID: bankID, faults: faults}
}

func (s *Server) Reserve(ctx context.Context, req *bankv1.ReserveRequest) (*bankv1.ReserveResponse, error) {
	if err := s.faults.BeforeReserve(); err != nil {
		return nil, status.Error(codes.Unavailable, err.Error())
	}
	if req.Amount == nil {
		return nil, status.Error(codes.InvalidArgument, "amount required")
	}

	res, err := s.svc.Reserve(ctx, req.TransferId, req.AccountId, req.Amount.AmountMinor, req.Amount.Currency, time.Unix(req.ExpiresAtUnix, 0))
	if err != nil {
		return nil, mapBankError(err)
	}
	return &bankv1.ReserveResponse{
		ReservationId: res.ID,
		BankId:        s.bankID,
	}, nil
}

func (s *Server) Commit(ctx context.Context, req *bankv1.CommitRequest) (*bankv1.CommitResponse, error) {
	if err := s.faults.BeforeCommit(); err != nil {
		return nil, status.Error(codes.Unavailable, err.Error())
	}
	if err := s.svc.Commit(ctx, req.ReservationId); err != nil {
		return nil, mapBankError(err)
	}
	return &bankv1.CommitResponse{}, nil
}

func (s *Server) Rollback(ctx context.Context, req *bankv1.RollbackRequest) (*bankv1.RollbackResponse, error) {
	if err := s.svc.Rollback(ctx, req.ReservationId); err != nil {
		return nil, mapBankError(err)
	}
	return &bankv1.RollbackResponse{}, nil
}

func (s *Server) Reconcile(ctx context.Context, req *bankv1.ReconcileRequest) (*bankv1.ReconcileResponse, error) {
	ids, err := s.svc.Reconcile(ctx, time.Unix(req.FromUnix, 0), time.Unix(req.ToUnix, 0))
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &bankv1.ReconcileResponse{TransferIds: ids}, nil
}

func (s *Server) GetBalance(ctx context.Context, req *bankv1.GetBalanceRequest) (*bankv1.GetBalanceResponse, error) {
	bal, cur, err := s.svc.Balance(ctx, req.AccountId)
	if err != nil {
		return nil, mapBankError(err)
	}
	return &bankv1.GetBalanceResponse{
		Balance: &bankv1.Money{AmountMinor: bal, Currency: cur},
	}, nil
}

func (s *Server) CreateAccount(ctx context.Context, req *bankv1.CreateAccountRequest) (*bankv1.CreateAccountResponse, error) {
	if req.OpeningBalance == nil {
		return nil, status.Error(codes.InvalidArgument, "opening_balance required")
	}
	if err := s.svc.CreateAccount(ctx, req.AccountId, req.OpeningBalance.Currency, req.OpeningBalance.AmountMinor); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &bankv1.CreateAccountResponse{}, nil
}

func mapBankError(err error) error {
	switch {
	case errors.Is(err, ErrAccountNotFound):
		return status.Error(codes.NotFound, err.Error())
	case errors.Is(err, ErrInsufficient):
		return status.Error(codes.FailedPrecondition, err.Error())
	case errors.Is(err, ErrCurrencyMatch):
		return status.Error(codes.InvalidArgument, err.Error())
	case errors.Is(err, ErrReservationGone):
		return status.Error(codes.NotFound, err.Error())
	default:
		return status.Error(codes.Internal, err.Error())
	}
}
