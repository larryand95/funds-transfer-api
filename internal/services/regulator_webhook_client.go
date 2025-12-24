package services

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/array/banking-api/internal/config"
	"github.com/array/banking-api/internal/models"
)

// RegulatorWebhookClient handles HTTP requests to the regulator webhook endpoint
type RegulatorWebhookClient struct {
	config config.RegulatorConfig
	client *http.Client
	logger *slog.Logger
}

// NewRegulatorWebhookClient creates a new regulator webhook client
func NewRegulatorWebhookClient(cfg config.RegulatorConfig, logger *slog.Logger) *RegulatorWebhookClient {
	if logger == nil {
		logger = slog.Default()
	}

	// Create HTTP client with timeout
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 5 * time.Second
	}

	client := &http.Client{
		Timeout: timeout,
	}

	return &RegulatorWebhookClient{
		config: cfg,
		client: client,
		logger: logger,
	}
}

// SendWebhook sends a webhook notification to the regulator
func (c *RegulatorWebhookClient) SendWebhook(payload models.JSONBMap) (*WebhookResponse, error) {
	if !c.config.Enabled {
		return nil, fmt.Errorf("regulator webhook notifications are disabled")
	}

	if c.config.WebhookURL == "" {
		return nil, fmt.Errorf("regulator webhook URL is not configured")
	}

	// Marshal payload to JSON
	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal payload: %w", err)
	}

	// Create HTTP request
	req, err := http.NewRequest("POST", c.config.WebhookURL, bytes.NewBuffer(jsonPayload))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Array-Banking-API/1.0")

	// Add webhook signature if enabled
	if c.config.EnableSigning && c.config.WebhookSecret != "" {
		signature := c.generateSignature(jsonPayload)
		req.Header.Set("X-Webhook-Signature", signature)
	}

	// Send request
	startTime := time.Now()
	resp, err := c.client.Do(req)
	duration := time.Since(startTime)

	if err != nil {
		c.logger.Warn("Failed to send webhook",
			"url", c.config.WebhookURL,
			"error", err,
			"duration", duration)
		return &WebhookResponse{
			Success:      false,
			ErrorMessage: err.Error(),
			Duration:     duration,
		}, err
	}
	defer resp.Body.Close()

	// Read response body
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		c.logger.Warn("Failed to read response body",
			"status_code", resp.StatusCode,
			"error", err)
	}

	// Parse response headers
	responseHeaders := make(models.JSONBMap)
	for key, values := range resp.Header {
		if len(values) > 0 {
			responseHeaders[key] = values[0]
		}
	}

	responseBodyStr := string(responseBody)
	success := resp.StatusCode >= 200 && resp.StatusCode < 300

	if success {
		c.logger.Info("Webhook sent successfully",
			"url", c.config.WebhookURL,
			"status_code", resp.StatusCode,
			"duration", duration)
	} else {
		c.logger.Warn("Webhook returned error status",
			"url", c.config.WebhookURL,
			"status_code", resp.StatusCode,
			"response_body", responseBodyStr,
			"duration", duration)
	}

	return &WebhookResponse{
		Success:         success,
		HTTPStatusCode:  resp.StatusCode,
		ResponseBody:    &responseBodyStr,
		ResponseHeaders: responseHeaders,
		Duration:        duration,
	}, nil
}

// generateSignature generates HMAC-SHA256 signature for webhook payload
func (c *RegulatorWebhookClient) generateSignature(payload []byte) string {
	mac := hmac.New(sha256.New, []byte(c.config.WebhookSecret))
	mac.Write(payload)
	signature := mac.Sum(nil)
	return hex.EncodeToString(signature)
}

// WebhookResponse represents the response from a webhook request
type WebhookResponse struct {
	Success         bool
	HTTPStatusCode  int
	ResponseBody    *string
	ResponseHeaders models.JSONBMap
	ErrorMessage    string
	Duration        time.Duration
}

