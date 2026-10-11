package rest

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/thomas666-beast/nexus-payments/internal/domain"
	"github.com/thomas666-beast/nexus-payments/internal/platform/metrics"
	"github.com/thomas666-beast/nexus-payments/internal/service"
)

const idempotencyKeyHeader = "Idempotency-Key"
const maxBodyBytes = 64 * 1024

type Handler struct {
	svc *service.Service
}

func NewHandler(svc *service.Service) *Handler {
	return &Handler{svc: svc}
}

// Routes registers endpoints and wraps them with middleware.
// Pass nil for m to skip metrics (useful in tests).
func (h *Handler) Routes(m *metrics.Metrics) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/transfers", h.initiateTransfer)
	mux.HandleFunc("GET /v1/transfers/{id}", h.getTransfer)
	mux.HandleFunc("GET /v1/accounts/{id}/balance", h.getBalance)

	if m == nil {
		return WithRequestID(mux)
	}
	return WithRequestID(WithMetrics(m, mux))
}

func (h *Handler) initiateTransfer(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorDTO{Error: "cannot read body", Code: "invalid_body"})
		return
	}
	var dto InitiateTransferRequestDTO
	if err := json.Unmarshal(body, &dto); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorDTO{Error: "invalid JSON", Code: "invalid_json"})
		return
	}

	req := service.ExecuteRequest{
		FromAccount:    domain.AccountID(dto.FromAccount),
		ToAccount:      domain.AccountID(dto.ToAccount),
		Amount:         domain.Money{Amount: dto.Amount.AmountMinor, Currency: domain.Currency(dto.Amount.Currency)},
		Reason:         dto.Reason,
		IdempotencyKey: strings.TrimSpace(r.Header.Get(idempotencyKeyHeader)),
	}

	res, err := h.svc.Execute(r.Context(), req)
	if err != nil {
		code, dto := errorResponse(err)
		writeJSON(w, code, dto)
		return
	}

	entryIDs := make([]string, 0, len(res.Entries))
	for _, e := range res.Entries {
		entryIDs = append(entryIDs, string(e.ID))
	}

	writeJSON(w, http.StatusCreated, InitiateTransferResponseDTO{
		TransferID:  string(res.Transfer.ID),
		Status:      string(res.Transfer.Status),
		Amount:      MoneyDTO{AmountMinor: res.Transfer.Amount.Amount, Currency: string(res.Transfer.Amount.Currency)},
		FromAccount: string(res.Transfer.FromAccount),
		ToAccount:   string(res.Transfer.ToAccount),
		EntryIDs:    entryIDs,
	})
}

func (h *Handler) getTransfer(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, ErrorDTO{Error: "transfer id required", Code: "invalid_request"})
		return
	}
	t, err := h.svc.GetTransfer(r.Context(), domain.TransferID(id))
	if err != nil {
		code, dto := errorResponse(err)
		writeJSON(w, code, dto)
		return
	}
	writeJSON(w, http.StatusOK, TransferStatusResponseDTO{
		TransferID:  string(t.ID),
		Status:      string(t.Status),
		Reason:      t.Reason,
		Amount:      MoneyDTO{AmountMinor: t.Amount.Amount, Currency: string(t.Amount.Currency)},
		FromAccount: string(t.FromAccount),
		ToAccount:   string(t.ToAccount),
	})
}

func (h *Handler) getBalance(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, ErrorDTO{Error: "account id required", Code: "invalid_request"})
		return
	}
	bal, err := h.svc.GetBalance(r.Context(), domain.AccountID(id))
	if err != nil {
		code, dto := errorResponse(err)
		writeJSON(w, code, dto)
		return
	}
	writeJSON(w, http.StatusOK, BalanceResponseDTO{
		Balance: MoneyDTO{AmountMinor: bal.Amount, Currency: string(bal.Currency)},
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
