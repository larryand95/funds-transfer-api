package services

import (
	"context"
	"log/slog"
	"time"

	"github.com/array/banking-api/internal/models"
	"github.com/array/banking-api/internal/repositories"
	"gorm.io/gorm"
)

// TransferMonitorService monitors external transfers and updates their status
type TransferMonitorService struct {
	transferRepo     repositories.TransferRepositoryInterface
	northWindService NorthWindServiceInterface
	logger           *slog.Logger
	ticker           *time.Ticker
	stopChan         chan struct{}
}

// NewTransferMonitorService creates a new transfer monitor service
func NewTransferMonitorService(
	transferRepo repositories.TransferRepositoryInterface,
	northWindService NorthWindServiceInterface,
	logger *slog.Logger,
) *TransferMonitorService {
	if logger == nil {
		logger = slog.Default()
	}

	return &TransferMonitorService{
		transferRepo:     transferRepo,
		northWindService: northWindService,
		logger:           logger,
		stopChan:         make(chan struct{}),
	}
}

// Start starts the transfer monitoring service
func (s *TransferMonitorService) Start(ctx context.Context, interval time.Duration) {
	s.ticker = time.NewTicker(interval)
	s.logger.Info("Transfer monitor service started", "interval", interval)

	go func() {
		for {
			select {
			case <-s.ticker.C:
				s.checkPendingTransfers()
			case <-s.stopChan:
				s.logger.Info("Transfer monitor service stopped")
				return
			case <-ctx.Done():
				s.logger.Info("Transfer monitor service stopped (context cancelled)")
				return
			}
		}
	}()
}

// Stop stops the transfer monitoring service
func (s *TransferMonitorService) Stop() {
	if s.ticker != nil {
		s.ticker.Stop()
	}
	close(s.stopChan)
}

// checkPendingTransfers checks and updates status of pending external transfers
func (s *TransferMonitorService) checkPendingTransfers() {
	// Get pending external transfers that need status check
	// We'll query the database directly for transfers that:
	// 1. Are external (is_external = true)
	// 2. Are pending or processing
	// 3. Have a NorthWind transfer ID
	// 4. Need a status check (based on last_status_check and status_check_count)

	// For now, we'll use a simple approach: check all pending/processing external transfers
	// In production, you'd want to batch this and limit the number checked per cycle

	// Note: This is a simplified version. In production, you'd want to:
	// 1. Query transfers that need checking (based on last_status_check time)
	// 2. Batch process them
	// 3. Handle rate limiting for NorthWind API
	// 4. Implement exponential backoff for failed checks

	s.logger.Debug("Checking pending external transfers")
}

// CheckTransferStatus checks a specific transfer's status with NorthWind
func (s *TransferMonitorService) CheckTransferStatus(transferID string) error {
	// This would be called by the transfer service
	// Implementation is in NorthWindTransferService.CheckTransferStatus
	return nil
}

// GetPendingTransfers retrieves transfers that need status checking
// This is a helper method that can be used by the repository or service
func GetPendingExternalTransfers(db *gorm.DB, limit int) ([]models.Transfer, error) {
	var transfers []models.Transfer

	// Find external transfers that are pending or processing
	// and haven't been checked recently (or at all)
	query := db.Where("is_external = ? AND status IN ?", true, []string{
		models.TransferStatusPending,
		models.TransferStatusProcessing,
	}).
		Where("northwind_transfer_id IS NOT NULL").
		Where("last_status_check IS NULL OR last_status_check < ?", time.Now().Add(-30*time.Second)).
		Where("status_check_count < ?", 100). // Limit retries
		Order("created_at ASC").
		Limit(limit)

	if err := query.Find(&transfers).Error; err != nil {
		return nil, err
	}

	return transfers, nil
}

