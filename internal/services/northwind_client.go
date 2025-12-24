package services

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/array/banking-api/internal/config"
	"github.com/array/banking-api/internal/dto"
)

var (
	ErrNorthWindAuthFailed    = errors.New("NorthWind authentication failed")
	ErrNorthWindNotConfigured = errors.New("NorthWind API not configured")
	ErrNorthWindAPIError      = errors.New("NorthWind API error")
	ErrNorthWindTimeout       = errors.New("NorthWind API request timeout")
)

// NorthWindClient handles all communication with NorthWind Bank API
type NorthWindClient struct {
	config       config.NorthWindConfig
	httpClient   *http.Client
	accessToken  string
	tokenExpires time.Time
	tokenMutex   sync.RWMutex
	logger       *slog.Logger
}

// NewNorthWindClient creates a new NorthWind API client
func NewNorthWindClient(cfg config.NorthWindConfig, logger *slog.Logger) *NorthWindClient {
	if logger == nil {
		logger = slog.Default()
	}

	return &NorthWindClient{
		config: cfg,
		httpClient: &http.Client{
			Timeout: cfg.Timeout,
		},
		logger: logger,
	}
}

// Authenticate authenticates with NorthWind API and stores the access token
func (c *NorthWindClient) Authenticate() error {
	if !c.config.EnableAuth {
		c.logger.Warn("NorthWind authentication is disabled")
		return nil
	}

	if c.config.BaseURL == "" || c.config.APIKey == "" {
		return fmt.Errorf("%w: missing required configuration (BaseURL or APIKey)", ErrNorthWindNotConfigured)
	}

	// Check if we have a valid cached token
	c.tokenMutex.RLock()
	if c.accessToken != "" && time.Now().Before(c.tokenExpires.Add(-5*time.Minute)) {
		c.tokenMutex.RUnlock()
		c.logger.Debug("Using cached NorthWind access token")
		return nil
	}
	c.tokenMutex.RUnlock()

	// Request new token
	authURL := fmt.Sprintf("%s/auth/token", c.config.BaseURL)

	var reqBody interface{}
	var jsonData []byte
	var err error

	// If API secret is provided, use key+secret authentication
	// Otherwise, use API key only (sent as header)
	if c.config.APISecret != "" {
		reqBody = dto.NorthWindAuthRequest{
			APIKey:    c.config.APIKey,
			APISecret: c.config.APISecret,
		}
		jsonData, err = json.Marshal(reqBody)
		if err != nil {
			return fmt.Errorf("failed to marshal auth request: %w", err)
		}
	} else {
		// API key only - send in request body or header depending on NorthWind API
		reqBody = map[string]string{
			"api_key": c.config.APIKey,
		}
		jsonData, err = json.Marshal(reqBody)
		if err != nil {
			return fmt.Errorf("failed to marshal auth request: %w", err)
		}
	}

	req, err := http.NewRequest("POST", authURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create auth request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	// If API secret is provided, add HMAC signature
	// Otherwise, use API key in Authorization header
	if c.config.APISecret != "" {
		signature := c.generateSignature(req, jsonData)
		if signature != "" {
			req.Header.Set("X-API-Signature", signature)
			req.Header.Set("X-API-Key", c.config.APIKey)
			req.Header.Set("X-Timestamp", fmt.Sprintf("%d", time.Now().Unix()))
		}
	} else {
		// API key only - use Bearer token or X-API-Key header
		// Try both common patterns
		req.Header.Set("X-API-Key", c.config.APIKey)
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.config.APIKey))
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to authenticate with NorthWind: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		c.logger.Error("NorthWind authentication failed",
			"status", resp.StatusCode,
			"response", string(body))
		return fmt.Errorf("%w: status %d", ErrNorthWindAuthFailed, resp.StatusCode)
	}

	var authResp dto.NorthWindAuthResponse
	if err := json.NewDecoder(resp.Body).Decode(&authResp); err != nil {
		return fmt.Errorf("failed to decode auth response: %w", err)
	}

	// Store token with expiration
	c.tokenMutex.Lock()
	c.accessToken = authResp.AccessToken
	if authResp.ExpiresIn > 0 {
		c.tokenExpires = time.Now().Add(time.Duration(authResp.ExpiresIn) * time.Second)
	} else {
		// Default to 1 hour if not specified
		c.tokenExpires = time.Now().Add(1 * time.Hour)
	}
	c.tokenMutex.Unlock()

	c.logger.Info("Successfully authenticated with NorthWind",
		"expires_at", c.tokenExpires)

	return nil
}

// generateSignature generates HMAC signature for request authentication
// Only used when API secret is provided
func (c *NorthWindClient) generateSignature(req *http.Request, body []byte) string {
	if c.config.APISecret == "" {
		return "" // No signature if no secret provided
	}

	// Create signature string: method + path + timestamp + body
	timestamp := req.Header.Get("X-Timestamp")
	if timestamp == "" {
		timestamp = fmt.Sprintf("%d", time.Now().Unix())
	}

	signatureString := fmt.Sprintf("%s%s%s%s",
		req.Method,
		req.URL.Path,
		timestamp,
		string(body))

	// Generate HMAC-SHA256
	mac := hmac.New(sha256.New, []byte(c.config.APISecret))
	mac.Write([]byte(signatureString))
	signature := hex.EncodeToString(mac.Sum(nil))

	return signature
}

// getAccessToken returns the current access token, refreshing if needed
func (c *NorthWindClient) getAccessToken() (string, error) {
	c.tokenMutex.RLock()
	hasValidToken := c.accessToken != "" && time.Now().Before(c.tokenExpires.Add(-5*time.Minute))
	c.tokenMutex.RUnlock()

	if !hasValidToken {
		if err := c.Authenticate(); err != nil {
			return "", err
		}
	}

	c.tokenMutex.RLock()
	defer c.tokenMutex.RUnlock()
	return c.accessToken, nil
}

// makeRequest makes an authenticated request to NorthWind API
func (c *NorthWindClient) makeRequest(method, endpoint string, body interface{}) (*http.Response, error) {
	// Ensure we're authenticated
	token, err := c.getAccessToken()
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s%s", c.config.BaseURL, endpoint)

	var reqBody io.Reader
	if body != nil {
		jsonData, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request body: %w", err)
		}
		reqBody = bytes.NewBuffer(jsonData)
	}

	req, err := http.NewRequest(method, url, reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	// Use Bearer token if we have one, otherwise use API key
	if token != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))
	} else if c.config.APIKey != "" {
		// Fallback to API key if no token (for API key-only auth)
		req.Header.Set("X-API-Key", c.config.APIKey)
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.config.APIKey))
	}

	// Retry logic
	var resp *http.Response
	var lastErr error
	for attempt := 0; attempt <= c.config.RetryAttempts; attempt++ {
		if attempt > 0 {
			time.Sleep(c.config.RetryDelay * time.Duration(attempt))
			c.logger.Debug("Retrying NorthWind API request",
				"attempt", attempt+1,
				"endpoint", endpoint)
		}

		resp, lastErr = c.httpClient.Do(req)
		if lastErr == nil && resp.StatusCode < 500 {
			break
		}

		if resp != nil {
			resp.Body.Close()
		}

		// If it's an auth error, try re-authenticating once
		if resp != nil && resp.StatusCode == http.StatusUnauthorized && attempt == 0 {
			c.tokenMutex.Lock()
			c.accessToken = ""
			c.tokenMutex.Unlock()
			if err := c.Authenticate(); err != nil {
				return nil, fmt.Errorf("re-authentication failed: %w", err)
			}
			token, _ = c.getAccessToken()
			req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))
		}
	}

	if lastErr != nil {
		return nil, fmt.Errorf("%w: %v", ErrNorthWindTimeout, lastErr)
	}

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		c.logger.Error("NorthWind API error",
			"status", resp.StatusCode,
			"endpoint", endpoint,
			"response", string(body))
		return nil, fmt.Errorf("%w: status %d, response: %s", ErrNorthWindAPIError, resp.StatusCode, string(body))
	}

	return resp, nil
}

// IsAuthenticated checks if the client has a valid authentication token
func (c *NorthWindClient) IsAuthenticated() bool {
	c.tokenMutex.RLock()
	defer c.tokenMutex.RUnlock()
	return c.accessToken != "" && time.Now().Before(c.tokenExpires)
}

// VerifyAccount verifies an external account at NorthWind Bank
func (c *NorthWindClient) VerifyAccount(req dto.NorthWindAccountVerificationRequest) (*dto.NorthWindAccountVerificationResponse, error) {
	resp, err := c.makeRequest("POST", "/api/v1/accounts/verify", req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var verificationResp dto.NorthWindAccountVerificationResponse
	if err := json.NewDecoder(resp.Body).Decode(&verificationResp); err != nil {
		return nil, fmt.Errorf("failed to decode verification response: %w", err)
	}

	return &verificationResp, nil
}

// InitiateTransfer initiates a transfer to a NorthWind account
func (c *NorthWindClient) InitiateTransfer(req dto.NorthWindTransferRequest) (*dto.NorthWindTransferResponse, error) {
	resp, err := c.makeRequest("POST", "/api/v1/transfers", req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var transferResp dto.NorthWindTransferResponse
	if err := json.NewDecoder(resp.Body).Decode(&transferResp); err != nil {
		return nil, fmt.Errorf("failed to decode transfer response: %w", err)
	}

	return &transferResp, nil
}

// GetTransferStatus checks the status of a transfer
func (c *NorthWindClient) GetTransferStatus(transferID string) (*dto.NorthWindTransferStatusResponse, error) {
	endpoint := fmt.Sprintf("/api/v1/transfers/%s/status", transferID)
	resp, err := c.makeRequest("GET", endpoint, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var statusResp dto.NorthWindTransferStatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&statusResp); err != nil {
		return nil, fmt.Errorf("failed to decode transfer status response: %w", err)
	}

	return &statusResp, nil
}
