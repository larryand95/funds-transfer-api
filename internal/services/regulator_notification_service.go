package services

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/array/banking-api/internal/config"
	"github.com/array/banking-api/internal/models"
	"github.com/array/banking-api/internal/repositories"
	"github.com/google/uuid"
)

// RegulatorNotificationService handles regulator webhook notifications
type RegulatorNotificationService struct {
	notificationRepo repositories.RegulatorNotificationRepositoryInterface
	webhookClient    *RegulatorWebhookClient
	config           config.RegulatorConfig
	logger           *slog.Logger
}

// NewRegulatorNotificationService creates a new regulator notification service
func NewRegulatorNotificationService(
	notificationRepo repositories.RegulatorNotificationRepositoryInterface,
	cfg config.RegulatorConfig,
	logger *slog.Logger,
) *RegulatorNotificationService {
	if logger == nil {
		logger = slog.Default()
	}

	webhookClient := NewRegulatorWebhookClient(cfg, logger)

	return &RegulatorNotificationService{
		notificationRepo: notificationRepo,
		webhookClient:    webhookClient,
		config:           cfg,
		logger:           logger,
	}
}

// NotifyTransferCompleted notifies the regulator when a transfer is completed
func (s *RegulatorNotificationService) NotifyTransferCompleted(transfer *models.Transfer) error {
	if !s.config.Enabled {
		s.logger.Debug("Regulator notifications disabled, skipping")
		return nil
	}

	payload := s.buildTransferCompletedPayload(transfer)
	return s.createAndSendNotification(transfer.ID, models.RegulatorNotificationEventTypeTransferCompleted, payload)
}

// NotifyTransferFailed notifies the regulator when a transfer fails
func (s *RegulatorNotificationService) NotifyTransferFailed(transfer *models.Transfer) error {
	if !s.config.Enabled {
		s.logger.Debug("Regulator notifications disabled, skipping")
		return nil
	}

	payload := s.buildTransferFailedPayload(transfer)
	return s.createAndSendNotification(transfer.ID, models.RegulatorNotificationEventTypeTransferFailed, payload)
}

// createAndSendNotification creates a notification record and sends it asynchronously
func (s *RegulatorNotificationService) createAndSendNotification(
	transferID uuid.UUID,
	eventType string,
	payload models.JSONBMap,
) error {
	// Create notification record
	notification := &models.RegulatorNotification{
		TransferID: transferID,
		EventType:   eventType,
		WebhookURL:  s.config.WebhookURL,
		Payload:     payload,
		Status:      models.RegulatorNotificationStatusPending,
		MaxAttempts: s.config.MaxRetries,
		DeadlineAt:  time.Now().Add(60 * time.Second),
	}

	// Set headers if signing is enabled
	if s.config.EnableSigning {
		notification.Headers = models.JSONBMap{
			"Content-Type": "application/json",
		}
	}

	// Save to database
	if err := s.notificationRepo.Create(notification); err != nil {
		return fmt.Errorf("failed to create notification record: %w", err)
	}

	// Send webhook asynchronously
	go s.SendWebhook(notification)

	return nil
}

// SendWebhook sends the webhook notification (public for worker access)
func (s *RegulatorNotificationService) SendWebhook(notification *models.RegulatorNotification) {
	// Mark as sending
	notification.MarkAsSending()
	if err := s.notificationRepo.Update(notification); err != nil {
		s.logger.Error("Failed to update notification status to sending",
			"notification_id", notification.ID,
			"error", err)
		return
	}

	// Send webhook
	response, err := s.webhookClient.SendWebhook(notification.Payload)

	// Update notification based on response
	if err != nil {
		// Network error or timeout
		errorMsg := err.Error()
		notification.MarkAsFailed(errorMsg, nil, nil)
		s.logger.Error("Failed to send webhook",
			"notification_id", notification.ID,
			"transfer_id", notification.TransferID,
			"error", err)
	} else if response.Success {
		// Success
		notification.MarkAsSent(
			response.HTTPStatusCode,
			response.ResponseBody,
			response.ResponseHeaders,
		)
		s.logger.Info("Webhook sent successfully",
			"notification_id", notification.ID,
			"transfer_id", notification.TransferID,
			"status_code", response.HTTPStatusCode,
			"notified_within_deadline", notification.NotifiedWithinDeadline)
	} else {
		// HTTP error
		errorMsg := fmt.Sprintf("HTTP %d", response.HTTPStatusCode)
		if response.ResponseBody != nil {
			errorMsg = fmt.Sprintf("%s: %s", errorMsg, *response.ResponseBody)
		}

		// Don't retry 4xx errors (client errors)
		if response.HTTPStatusCode >= 400 && response.HTTPStatusCode < 500 {
			notification.Status = models.RegulatorNotificationStatusFailedPermanently
			now := time.Now()
			notification.CompletedAt = &now
		} else {
			// Retry 5xx errors
			notification.MarkAsFailed(errorMsg, &response.HTTPStatusCode, response.ResponseBody)
		}

		s.logger.Warn("Webhook returned error",
			"notification_id", notification.ID,
			"transfer_id", notification.TransferID,
			"status_code", response.HTTPStatusCode,
			"error", errorMsg)
	}

	// Update notification in database
	if err := s.notificationRepo.Update(notification); err != nil {
		s.logger.Error("Failed to update notification after webhook attempt",
			"notification_id", notification.ID,
			"error", err)
	}
}

// RetryFailedNotification retries a failed notification
func (s *RegulatorNotificationService) RetryFailedNotification(notification *models.RegulatorNotification) error {
	if !notification.NeedsRetry() {
		return fmt.Errorf("notification does not need retry: status=%s, attempts=%d/%d",
			notification.Status, notification.AttemptCount, notification.MaxAttempts)
	}

	s.logger.Info("Retrying webhook notification",
		"notification_id", notification.ID,
		"transfer_id", notification.TransferID,
		"attempt", notification.AttemptCount+1,
		"max_attempts", notification.MaxAttempts)

	// Send webhook
	s.SendWebhook(notification)

	return nil
}

// buildTransferCompletedPayload builds the webhook payload for a completed transfer
func (s *RegulatorNotificationService) buildTransferCompletedPayload(transfer *models.Transfer) models.JSONBMap {
	payload := models.JSONBMap{
		"event_type": models.RegulatorNotificationEventTypeTransferCompleted,
		"event_id":    uuid.New().String(),
		"timestamp":   time.Now().UTC().Format(time.RFC3339),
		"transfer":    s.buildTransferPayload(transfer),
	}

	// Add completion time
	if transfer.CompletedAt != nil {
		payload["transfer"].(models.JSONBMap)["completed_at"] = transfer.CompletedAt.UTC().Format(time.RFC3339)
	}

	return payload
}

// buildTransferFailedPayload builds the webhook payload for a failed transfer
func (s *RegulatorNotificationService) buildTransferFailedPayload(transfer *models.Transfer) models.JSONBMap {
	payload := models.JSONBMap{
		"event_type": models.RegulatorNotificationEventTypeTransferFailed,
		"event_id":    uuid.New().String(),
		"timestamp":   time.Now().UTC().Format(time.RFC3339),
		"transfer":    s.buildTransferPayload(transfer),
	}

	// Add failure details
	transferPayload := payload["transfer"].(models.JSONBMap)
	if transfer.FailedAt != nil {
		transferPayload["failed_at"] = transfer.FailedAt.UTC().Format(time.RFC3339)
	}
	if transfer.ErrorMessage != nil {
		transferPayload["failure_reason"] = *transfer.ErrorMessage
	}

	return payload
}

// buildTransferPayload builds the transfer portion of the webhook payload
func (s *RegulatorNotificationService) buildTransferPayload(transfer *models.Transfer) models.JSONBMap {
	payload := models.JSONBMap{
		"transfer_id":    transfer.ID.String(),
		"from_account_id": transfer.FromAccountID.String(),
		"amount":         transfer.Amount.String(),
		"currency":       "USD", // Default, should come from account
		"status":         transfer.Status,
		"description":    transfer.Description,
		"created_at":     transfer.CreatedAt.UTC().Format(time.RFC3339),
	}

	// Add to_account_id if internal transfer
	if transfer.ToAccountID != nil {
		payload["to_account_id"] = transfer.ToAccountID.String()
	}

	// Add external transfer details
	if transfer.IsExternal {
		payload["is_external"] = true
		payload["transfer_type"] = transfer.TransferType
		if transfer.ExternalAccountID != nil {
			payload["external_account_id"] = transfer.ExternalAccountID.String()
		}
		if transfer.NorthWindTransferID != nil {
			payload["northwind_transfer_id"] = *transfer.NorthWindTransferID
		}
	}

	return payload
}

// GetNotificationStatus retrieves the notification status for a transfer
func (s *RegulatorNotificationService) GetNotificationStatus(transferID uuid.UUID) ([]models.RegulatorNotification, error) {
	return s.notificationRepo.FindByTransferID(transferID)
}

