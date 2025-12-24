package models

import (
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	ExternalAccountStatusPending  = "pending"
	ExternalAccountStatusVerified = "verified"
	ExternalAccountStatusFailed   = "failed"
	ExternalAccountStatusInactive = "inactive"
)

var (
	ErrInvalidExternalAccountStatus = errors.New("invalid external account status")
)

// ExternalAccount represents a NorthWind Bank account registered by a customer
type ExternalAccount struct {
	ID                 uuid.UUID      `gorm:"type:uuid;primary_key" json:"id"`
	UserID             uuid.UUID      `gorm:"type:uuid;not null;index" json:"user_id"`
	AccountNumber      string         `gorm:"type:varchar(50);not null" json:"account_number"`
	AccountName        string         `gorm:"type:varchar(255);not null" json:"account_name"`
	BankName           string         `gorm:"type:varchar(100);default:'NorthWind Bank'" json:"bank_name"`
	BankCode           string         `gorm:"type:varchar(20)" json:"bank_code,omitempty"`
	Currency           string         `gorm:"type:varchar(3);not null;default:'USD'" json:"currency"`
	NorthWindAccountID string         `gorm:"type:varchar(100);index" json:"northwind_account_id,omitempty"` // ID from NorthWind API
	Status             string         `gorm:"type:varchar(20);not null;default:'pending';index" json:"status"`
	Verified           bool           `gorm:"default:false" json:"verified"`
	VerificationError  *string        `gorm:"type:text" json:"verification_error,omitempty"`
	Nickname           string         `gorm:"type:varchar(100)" json:"nickname,omitempty"` // User-friendly name
	CreatedAt          time.Time      `gorm:"not null" json:"created_at"`
	UpdatedAt          time.Time      `gorm:"not null" json:"updated_at"`
	VerifiedAt         *time.Time     `json:"verified_at,omitempty"`
	DeletedAt          gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`

	// Associations
	User User `gorm:"foreignKey:UserID" json:"-"`
}

// BeforeCreate hook for ExternalAccount
func (e *ExternalAccount) BeforeCreate(tx *gorm.DB) error {
	if e.ID == uuid.Nil {
		e.ID = uuid.New()
	}

	if e.Status == "" {
		e.Status = ExternalAccountStatusPending
	}

	if e.Currency == "" {
		e.Currency = "USD"
	}

	if e.BankName == "" {
		e.BankName = "NorthWind Bank"
	}

	now := time.Now()
	if e.CreatedAt.IsZero() {
		e.CreatedAt = now
	}
	if e.UpdatedAt.IsZero() {
		e.UpdatedAt = now
	}

	return e.Validate()
}

// BeforeUpdate hook for ExternalAccount
func (e *ExternalAccount) BeforeUpdate(tx *gorm.DB) error {
	e.UpdatedAt = time.Now()
	return e.Validate()
}

// Validate validates the external account fields
func (e *ExternalAccount) Validate() error {
	if e.UserID == uuid.Nil {
		return errors.New("user ID is required")
	}

	if e.AccountNumber == "" {
		return errors.New("account number is required")
	}

	if e.AccountName == "" {
		return errors.New("account name is required")
	}

	if !IsValidExternalAccountStatus(e.Status) {
		return ErrInvalidExternalAccountStatus
	}

	return nil
}

// IsPending returns true if the account is pending verification
func (e *ExternalAccount) IsPending() bool {
	return e.Status == ExternalAccountStatusPending
}

// IsVerified returns true if the account is verified
func (e *ExternalAccount) IsVerified() bool {
	return e.Status == ExternalAccountStatusVerified && e.Verified
}

// IsActive returns true if the account can be used for transfers
func (e *ExternalAccount) IsActive() bool {
	return e.Status == ExternalAccountStatusVerified && !e.DeletedAt.Valid
}

// MarkAsVerified marks the account as verified
func (e *ExternalAccount) MarkAsVerified(northwindAccountID string) {
	e.Status = ExternalAccountStatusVerified
	e.Verified = true
	e.NorthWindAccountID = northwindAccountID
	now := time.Now()
	e.VerifiedAt = &now
	e.VerificationError = nil
}

// MarkAsFailed marks the account verification as failed
func (e *ExternalAccount) MarkAsFailed(errorMessage string) {
	e.Status = ExternalAccountStatusFailed
	e.Verified = false
	e.VerificationError = &errorMessage
}

// TableName returns the table name for ExternalAccount
func (e *ExternalAccount) TableName() string {
	return "external_accounts"
}

// IsValidExternalAccountStatus checks if the status is valid
func IsValidExternalAccountStatus(status string) bool {
	switch status {
	case ExternalAccountStatusPending, ExternalAccountStatusVerified, ExternalAccountStatusFailed, ExternalAccountStatusInactive:
		return true
	default:
		return false
	}
}
