package dto

import "github.com/google/uuid"

// RegisterExternalAccountRequest represents a request to register an external NorthWind account
type RegisterExternalAccountRequest struct {
	AccountNumber string `json:"account_number" validate:"required,min=1,max=50"`
	AccountName   string `json:"account_name" validate:"required,min=1,max=255"`
	BankCode      string `json:"bank_code,omitempty" validate:"omitempty,max=20"`
	Currency      string `json:"currency,omitempty" validate:"omitempty,len=3"`
	Nickname      string `json:"nickname,omitempty" validate:"omitempty,max=100"`
}

// RegisterExternalAccountResponse represents the response after registering an external account
type RegisterExternalAccountResponse struct {
	ID            uuid.UUID `json:"id"`
	AccountNumber string    `json:"account_number"`
	AccountName   string    `json:"account_name"`
	BankName      string    `json:"bank_name"`
	Currency      string    `json:"currency"`
	Status        string    `json:"status"`
	IsVerified    bool      `json:"is_verified"`
	Nickname      string    `json:"nickname,omitempty"`
	CreatedAt     string    `json:"created_at"`
}

// ExternalAccountResponse represents an external account in responses
type ExternalAccountResponse struct {
	ID                uuid.UUID `json:"id"`
	AccountNumber     string    `json:"account_number"`
	AccountName       string    `json:"account_name"`
	BankName          string    `json:"bank_name"`
	BankCode          string    `json:"bank_code,omitempty"`
	Currency          string    `json:"currency"`
	Status            string    `json:"status"`
	IsVerified        bool      `json:"is_verified"`
	VerificationError string    `json:"verification_error,omitempty"`
	Nickname          string    `json:"nickname,omitempty"`
	CreatedAt         string    `json:"created_at"`
	VerifiedAt        string    `json:"verified_at,omitempty"`
}

// InitiateExternalTransferRequest represents a request to initiate a transfer to NorthWind
type InitiateExternalTransferRequest struct {
	FromAccountID     uuid.UUID `json:"from_account_id" validate:"required"`
	ExternalAccountID uuid.UUID `json:"external_account_id" validate:"required"`
	Amount            string    `json:"amount" validate:"required,gt=0"`
	Currency          string    `json:"currency,omitempty" validate:"omitempty,len=3"`
	Description       string    `json:"description" validate:"required,min=1,max=500"`
	TransferType      string    `json:"transfer_type" validate:"required,oneof=instant standard scheduled"`
	ScheduledAt       string    `json:"scheduled_at,omitempty" validate:"omitempty"` // ISO 8601 format for scheduled transfers
	Reference         string    `json:"reference,omitempty" validate:"omitempty,max=100"`
}

// InitiateExternalTransferResponse represents the response after initiating an external transfer
type InitiateExternalTransferResponse struct {
	TransferID          uuid.UUID `json:"transfer_id"`
	NorthWindTransferID string    `json:"northwind_transfer_id,omitempty"`
	Status              string    `json:"status"`
	Amount              string    `json:"amount"`
	Currency            string    `json:"currency"`
	TransferType        string    `json:"transfer_type"`
	EstimatedCompletion string    `json:"estimated_completion,omitempty"`
	CreatedAt           string    `json:"created_at"`
	Message             string    `json:"message,omitempty"`
}

// ExternalTransferStatusResponse represents the status of an external transfer
type ExternalTransferStatusResponse struct {
	TransferID          uuid.UUID `json:"transfer_id"`
	NorthWindTransferID string    `json:"northwind_transfer_id,omitempty"`
	Status              string    `json:"status"`
	Amount              string    `json:"amount"`
	Currency            string    `json:"currency"`
	TransferType        string    `json:"transfer_type"`
	EstimatedCompletion string    `json:"estimated_completion,omitempty"`
	Progress            int       `json:"progress,omitempty"` // 0-100
	CreatedAt           string    `json:"created_at"`
	CompletedAt         string    `json:"completed_at,omitempty"`
	FailedAt            string    `json:"failed_at,omitempty"`
	FailureReason       string    `json:"failure_reason,omitempty"`
	LastStatusCheck     string    `json:"last_status_check,omitempty"`
}
