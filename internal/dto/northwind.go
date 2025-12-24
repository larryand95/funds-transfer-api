package dto

import "time"

// NorthWindAuthRequest represents the authentication request to NorthWind
// APISecret is optional - only required if NorthWind uses key+secret authentication
type NorthWindAuthRequest struct {
	APIKey    string `json:"api_key"`
	APISecret string `json:"api_secret,omitempty"` // Optional
}

// NorthWindAuthResponse represents the authentication response from NorthWind
type NorthWindAuthResponse struct {
	AccessToken string    `json:"access_token"`
	TokenType   string    `json:"token_type"`
	ExpiresIn   int       `json:"expires_in"` // seconds
	ExpiresAt   time.Time `json:"expires_at,omitempty"`
	Scope       string    `json:"scope,omitempty"`
}

// NorthWindErrorResponse represents an error response from NorthWind API
type NorthWindErrorResponse struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description,omitempty"`
	ErrorCode        string `json:"error_code,omitempty"`
	Message          string `json:"message,omitempty"`
}

// NorthWindAccount represents an external account at NorthWind Bank
type NorthWindAccount struct {
	AccountID     string `json:"account_id"`
	AccountNumber string `json:"account_number"`
	AccountType   string `json:"account_type"`
	Currency      string `json:"currency"`
	AccountName   string `json:"account_name,omitempty"`
	BankName      string `json:"bank_name,omitempty"`
	IsActive      bool   `json:"is_active"`
	Verified      bool   `json:"verified"`
}

// NorthWindTransferRequest represents a transfer request to NorthWind
type NorthWindTransferRequest struct {
	FromAccountID string `json:"from_account_id"`
	ToAccountID   string `json:"to_account_id"`
	Amount        string `json:"amount"` // Decimal as string
	Currency      string `json:"currency"`
	Description   string `json:"description,omitempty"`
	Reference     string `json:"reference,omitempty"`
	TransferType  string `json:"transfer_type"` // "instant", "standard", "scheduled"
}

// NorthWindTransferResponse represents a transfer response from NorthWind
type NorthWindTransferResponse struct {
	TransferID          string     `json:"transfer_id"`
	Status              string     `json:"status"` // "pending", "processing", "completed", "failed"
	FromAccountID       string     `json:"from_account_id"`
	ToAccountID         string     `json:"to_account_id"`
	Amount              string     `json:"amount"`
	Currency            string     `json:"currency"`
	CreatedAt           time.Time  `json:"created_at"`
	CompletedAt         *time.Time `json:"completed_at,omitempty"`
	FailedAt            *time.Time `json:"failed_at,omitempty"`
	FailureReason       string     `json:"failure_reason,omitempty"`
	EstimatedCompletion time.Time  `json:"estimated_completion,omitempty"`
}

// NorthWindTransferStatusResponse represents a transfer status check response
type NorthWindTransferStatusResponse struct {
	TransferID    string     `json:"transfer_id"`
	Status        string     `json:"status"`
	Amount        string     `json:"amount"`
	Currency      string     `json:"currency"`
	CreatedAt     time.Time  `json:"created_at"`
	CompletedAt   *time.Time `json:"completed_at,omitempty"`
	FailedAt      *time.Time `json:"failed_at,omitempty"`
	FailureReason string     `json:"failure_reason,omitempty"`
	Progress      int        `json:"progress,omitempty"` // 0-100 percentage
}

// NorthWindAccountVerificationRequest represents an account verification request
type NorthWindAccountVerificationRequest struct {
	AccountNumber string `json:"account_number"`
	AccountName   string `json:"account_name,omitempty"`
	BankCode      string `json:"bank_code,omitempty"`
}

// NorthWindAccountVerificationResponse represents an account verification response
type NorthWindAccountVerificationResponse struct {
	AccountID     string `json:"account_id"`
	AccountNumber string `json:"account_number"`
	AccountName   string `json:"account_name"`
	IsValid       bool   `json:"is_valid"`
	IsVerified    bool   `json:"is_verified"`
	Message       string `json:"message,omitempty"`
}
