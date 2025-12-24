package services

import (
	"log/slog"

	"github.com/array/banking-api/internal/config"
	"github.com/array/banking-api/internal/dto"
)

// NorthWindService wraps the NorthWind client and implements the service interface
type NorthWindService struct {
	client *NorthWindClient
	logger *slog.Logger
}

// NewNorthWindService creates a new NorthWind service
func NewNorthWindService(cfg config.NorthWindConfig, logger *slog.Logger) NorthWindServiceInterface {
	if logger == nil {
		logger = slog.Default()
	}

	client := NewNorthWindClient(cfg, logger)
	return &NorthWindService{
		client: client,
		logger: logger,
	}
}

// Authenticate authenticates with NorthWind API
func (s *NorthWindService) Authenticate() error {
	return s.client.Authenticate()
}

// IsAuthenticated checks if we have a valid authentication token
func (s *NorthWindService) IsAuthenticated() bool {
	return s.client.IsAuthenticated()
}

// VerifyAccount verifies an external account at NorthWind Bank
func (s *NorthWindService) VerifyAccount(req dto.NorthWindAccountVerificationRequest) (*dto.NorthWindAccountVerificationResponse, error) {
	return s.client.VerifyAccount(req)
}

// InitiateTransfer initiates a transfer to a NorthWind account
func (s *NorthWindService) InitiateTransfer(req dto.NorthWindTransferRequest) (*dto.NorthWindTransferResponse, error) {
	return s.client.InitiateTransfer(req)
}

// GetTransferStatus checks the status of a transfer
func (s *NorthWindService) GetTransferStatus(transferID string) (*dto.NorthWindTransferStatusResponse, error) {
	return s.client.GetTransferStatus(transferID)
}
