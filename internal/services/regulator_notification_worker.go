package services

import (
	"context"
	"log/slog"
	"time"

	"github.com/array/banking-api/internal/models"
	"github.com/array/banking-api/internal/repositories"
)

// RegulatorNotificationWorker processes failed webhook notifications and retries them
type RegulatorNotificationWorker struct {
	notificationRepo repositories.RegulatorNotificationRepositoryInterface
	notificationService *RegulatorNotificationService
	logger           *slog.Logger
	ticker           *time.Ticker
	stopChan         chan struct{}
	batchSize        int
	interval         time.Duration
}

// NewRegulatorNotificationWorker creates a new regulator notification worker
func NewRegulatorNotificationWorker(
	notificationRepo repositories.RegulatorNotificationRepositoryInterface,
	notificationService *RegulatorNotificationService,
	logger *slog.Logger,
) *RegulatorNotificationWorker {
	if logger == nil {
		logger = slog.Default()
	}

	return &RegulatorNotificationWorker{
		notificationRepo:    notificationRepo,
		notificationService: notificationService,
		logger:              logger,
		stopChan:            make(chan struct{}),
		batchSize:           50, // Process 50 notifications per cycle
		interval:            10 * time.Second, // Check every 10 seconds
	}
}

// Start starts the worker
func (w *RegulatorNotificationWorker) Start(ctx context.Context) {
	w.ticker = time.NewTicker(w.interval)
	w.logger.Info("Regulator notification worker started",
		"interval", w.interval,
		"batch_size", w.batchSize)

	go func() {
		for {
			select {
			case <-w.ticker.C:
				w.processRetries()
				w.processPending()
			case <-w.stopChan:
				w.logger.Info("Regulator notification worker stopped")
				return
			case <-ctx.Done():
				w.logger.Info("Regulator notification worker stopped (context cancelled)")
				return
			}
		}
	}()
}

// Stop stops the worker
func (w *RegulatorNotificationWorker) Stop() {
	if w.ticker != nil {
		w.ticker.Stop()
	}
	close(w.stopChan)
}

// processRetries processes notifications that need to be retried
func (w *RegulatorNotificationWorker) processRetries() {
	notifications, err := w.notificationRepo.FindRetryableNotifications(w.batchSize)
	if err != nil {
		w.logger.Error("Failed to find retryable notifications",
			"error", err)
		return
	}

	if len(notifications) == 0 {
		return
	}

	w.logger.Debug("Processing retryable notifications",
		"count", len(notifications))

	for i := range notifications {
		notification := &notifications[i]
		if err := w.notificationService.RetryFailedNotification(notification); err != nil {
			w.logger.Error("Failed to retry notification",
				"notification_id", notification.ID,
				"transfer_id", notification.TransferID,
				"error", err)
		}
	}
}

// processPending processes pending notifications that are approaching deadline
func (w *RegulatorNotificationWorker) processPending() {
	// Find pending notifications approaching deadline (within 30 seconds)
	threshold := 30 * time.Second
	notifications, err := w.notificationRepo.FindApproachingDeadline(threshold, w.batchSize)
	if err != nil {
		w.logger.Error("Failed to find pending notifications approaching deadline",
			"error", err)
		return
	}

	if len(notifications) == 0 {
		return
	}

	w.logger.Debug("Processing pending notifications approaching deadline",
		"count", len(notifications))

	for i := range notifications {
		notification := &notifications[i]
		// Send webhook
		go w.notificationService.SendWebhook(notification)
	}
}

// GetStats returns statistics about notification processing
func (w *RegulatorNotificationWorker) GetStats() (map[string]int64, error) {
	stats := make(map[string]int64)

	statuses := []string{
		models.RegulatorNotificationStatusPending,
		models.RegulatorNotificationStatusSending,
		models.RegulatorNotificationStatusSent,
		models.RegulatorNotificationStatusFailed,
		models.RegulatorNotificationStatusRetrying,
		models.RegulatorNotificationStatusFailedPermanently,
	}

	for _, status := range statuses {
		count, err := w.notificationRepo.CountByStatus(status)
		if err != nil {
			return nil, err
		}
		stats[status] = count
	}

	return stats, nil
}

