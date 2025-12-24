package models

import (
	"errors"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

const (
	TransferStatusPending    = "pending"
	TransferStatusCompleted  = "completed"
	TransferStatusFailed     = "failed"
	TransferStatusProcessing = "processing" // For external transfers being processed by NorthWind

	TransferTypeInstant   = "instant"
	TransferTypeStandard  = "standard"
	TransferTypeScheduled = "scheduled"
)

var (
	ErrInvalidTransferStatus = errors.New("invalid transfer status")
	ErrInvalidTransferAmount = errors.New("transfer amount must be positive")
)

// Transfer represents an account-to-account transfer (internal or external to NorthWind)
type Transfer struct {
	ID                  uuid.UUID       `gorm:"type:uuid;primary_key" json:"id"`
	FromAccountID       uuid.UUID       `gorm:"type:uuid;not null;index:idx_transfer_from_account" json:"from_account_id"`
	ToAccountID         *uuid.UUID      `gorm:"type:uuid;index:idx_transfer_to_account" json:"to_account_id,omitempty"` // NULL for external transfers
	Amount              decimal.Decimal `gorm:"type:decimal(15,2);not null" json:"amount"`
	Description         string          `gorm:"type:text;not null" json:"description"`
	IdempotencyKey      string          `gorm:"type:varchar(255);uniqueIndex;not null" json:"idempotency_key"`
	Status              string          `gorm:"type:varchar(20);not null;default:'pending';index:idx_transfer_status" json:"status"`
	DebitTransactionID  *uuid.UUID      `gorm:"type:uuid;index" json:"debit_transaction_id,omitempty"`
	CreditTransactionID *uuid.UUID      `gorm:"type:uuid;index" json:"credit_transaction_id,omitempty"`
	ErrorMessage        *string         `gorm:"type:text" json:"error_message,omitempty"`
	CreatedAt           time.Time       `gorm:"not null;index:idx_transfer_created_at" json:"created_at"`
	UpdatedAt           time.Time       `gorm:"not null" json:"updated_at"`
	CompletedAt         *time.Time      `json:"completed_at,omitempty"`
	FailedAt            *time.Time      `json:"failed_at,omitempty"`

	// External transfer fields (for NorthWind transfers)
	IsExternal          bool       `gorm:"default:false;index:idx_transfers_is_external" json:"is_external"`
	ExternalAccountID   *uuid.UUID `gorm:"type:uuid;index:idx_transfers_external_account_id" json:"external_account_id,omitempty"`
	NorthWindTransferID *string    `gorm:"type:varchar(100);index:idx_transfers_northwind_transfer_id" json:"northwind_transfer_id,omitempty"`
	TransferType        string     `gorm:"type:varchar(20);default:'standard';index:idx_transfers_transfer_type" json:"transfer_type"`
	ScheduledAt         *time.Time `json:"scheduled_at,omitempty"`
	EstimatedCompletion *time.Time `json:"estimated_completion,omitempty"`
	LastStatusCheck     *time.Time `json:"last_status_check,omitempty"`
	StatusCheckCount    int        `gorm:"default:0" json:"status_check_count"`

	// Associations
	FromAccount       Account          `gorm:"foreignKey:FromAccountID" json:"-"`
	ToAccount         *Account         `gorm:"foreignKey:ToAccountID" json:"-"`
	ExternalAccount   *ExternalAccount `gorm:"foreignKey:ExternalAccountID" json:"-"`
	DebitTransaction  *Transaction     `gorm:"foreignKey:DebitTransactionID" json:"-"`
	CreditTransaction *Transaction     `gorm:"foreignKey:CreditTransactionID" json:"-"`
}

// BeforeCreate hook for Transfer
func (t *Transfer) BeforeCreate(tx *gorm.DB) error {
	if t.ID == uuid.Nil {
		t.ID = uuid.New()
	}

	if t.Status == "" {
		t.Status = TransferStatusPending
	}

	now := time.Now()
	if t.CreatedAt.IsZero() {
		t.CreatedAt = now
	}
	if t.UpdatedAt.IsZero() {
		t.UpdatedAt = now
	}

	return t.Validate()
}

// BeforeUpdate hook for Transfer
func (t *Transfer) BeforeUpdate(tx *gorm.DB) error {
	t.UpdatedAt = time.Now()
	return t.Validate()
}

// Validate validates the transfer fields
func (t *Transfer) Validate() error {
	if t.FromAccountID == uuid.Nil {
		return errors.New("from account ID is required")
	}

	// For external transfers, external_account_id is required instead of to_account_id
	if t.IsExternal {
		if t.ExternalAccountID == nil || *t.ExternalAccountID == uuid.Nil {
			return errors.New("external account ID is required for external transfers")
		}
		if t.ToAccountID != nil {
			return errors.New("to account ID should not be set for external transfers")
		}
	} else {
		// For internal transfers, to_account_id is required
		if t.ToAccountID == nil || *t.ToAccountID == uuid.Nil {
			return errors.New("to account ID is required for internal transfers")
		}
		if t.FromAccountID == *t.ToAccountID {
			return errors.New("from and to accounts cannot be the same")
		}
	}

	if t.Amount.LessThanOrEqual(decimal.Zero) {
		return ErrInvalidTransferAmount
	}

	if t.Description == "" {
		return errors.New("description is required")
	}

	if t.IdempotencyKey == "" {
		return errors.New("idempotency key is required")
	}

	if !IsValidTransferStatus(t.Status) {
		return ErrInvalidTransferStatus
	}

	if t.IsExternal && !IsValidTransferType(t.TransferType) {
		return errors.New("invalid transfer type")
	}

	return nil
}

// IsPending returns true if the transfer is pending
func (t *Transfer) IsPending() bool {
	return t.Status == TransferStatusPending
}

// IsCompleted returns true if the transfer is completed
func (t *Transfer) IsCompleted() bool {
	return t.Status == TransferStatusCompleted
}

// IsFailed returns true if the transfer is failed
func (t *Transfer) IsFailed() bool {
	return t.Status == TransferStatusFailed
}

// Complete marks the transfer as completed and links transaction IDs
func (t *Transfer) Complete(debitTxID, creditTxID uuid.UUID) {
	t.Status = TransferStatusCompleted
	now := time.Now()
	t.CompletedAt = &now
	t.DebitTransactionID = &debitTxID
	t.CreditTransactionID = &creditTxID
}

// Fail marks the transfer as failed with an error message
func (t *Transfer) Fail(errorMessage string) {
	t.Status = TransferStatusFailed
	now := time.Now()
	t.FailedAt = &now
	t.ErrorMessage = &errorMessage
}

// CanTransitionTo checks if a transfer can transition to a new status
func (t *Transfer) CanTransitionTo(newStatus string) bool {
	validTransitions := map[string][]string{
		TransferStatusPending:    {TransferStatusProcessing, TransferStatusCompleted, TransferStatusFailed},
		TransferStatusProcessing: {TransferStatusCompleted, TransferStatusFailed},
		TransferStatusCompleted:  {},
		TransferStatusFailed:     {},
	}

	allowedStatuses, exists := validTransitions[t.Status]
	if !exists {
		return false
	}

	return slices.Contains(allowedStatuses, newStatus)
}

// MarkAsProcessing marks the transfer as being processed by NorthWind
func (t *Transfer) MarkAsProcessing(northwindTransferID string, estimatedCompletion *time.Time) {
	t.Status = TransferStatusProcessing
	t.NorthWindTransferID = &northwindTransferID
	if estimatedCompletion != nil {
		t.EstimatedCompletion = estimatedCompletion
	}
}

// TableName returns the table name for Transfer
func (t *Transfer) TableName() string {
	return "transfers"
}

// Helper functions

// IsValidTransferStatus checks if the transfer status is valid
func IsValidTransferStatus(status string) bool {
	switch status {
	case TransferStatusPending, TransferStatusProcessing, TransferStatusCompleted, TransferStatusFailed:
		return true
	default:
		return false
	}
}

// IsValidTransferType checks if the transfer type is valid
func IsValidTransferType(transferType string) bool {
	switch transferType {
	case TransferTypeInstant, TransferTypeStandard, TransferTypeScheduled:
		return true
	default:
		return false
	}
}

// IsExternalTransfer returns true if this is an external transfer
func (t *Transfer) IsExternalTransfer() bool {
	return t.IsExternal
}

// NeedsStatusCheck returns true if the transfer needs a status check
func (t *Transfer) NeedsStatusCheck() bool {
	if !t.IsExternal {
		return false
	}

	// Check if it's pending or processing
	if t.Status != TransferStatusPending && t.Status != TransferStatusProcessing {
		return false
	}

	// Check if enough time has passed since last check (at least 30 seconds)
	if t.LastStatusCheck != nil {
		timeSinceLastCheck := time.Since(*t.LastStatusCheck)
		if timeSinceLastCheck < 30*time.Second {
			return false
		}
	}

	return true
}
