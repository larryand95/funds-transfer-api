package main

//go:generate swag init -g main.go -o ../../docs --parseDependency --parseInternal -ot yaml,json --v3.1

// @title Array Banking API
// @version 1.0
// @description Production-quality banking REST API for developer assessment and interviewing. Provides core banking functionality including identity management, account operations, customer management, and transaction processing.
//
// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization
// @description Type "Bearer" followed by a space and JWT token.

import (
	"context"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/array/banking-api/internal/config"
	"github.com/array/banking-api/internal/database"
	"github.com/array/banking-api/internal/handlers"
	"github.com/array/banking-api/internal/middleware"
	"github.com/array/banking-api/internal/repositories"
	"github.com/array/banking-api/internal/services"
	"github.com/array/banking-api/internal/validation"
	"github.com/go-playground/validator/v10"
	"github.com/labstack/echo/v4"
	echomiddleware "github.com/labstack/echo/v4/middleware"
)

var cfg *config.Config

// CustomValidator implements echo.Validator interface using our validation package
type CustomValidator struct {
	validator *validator.Validate
}

// Validate validates the struct using go-playground validator
// Returns validation errors that will be caught by the error handling middleware
func (cv *CustomValidator) Validate(i interface{}) error {
	// Return raw validation error - the middleware will format it properly with trace IDs
	if err := cv.validator.Struct(i); err != nil {
		return err
	}

	return nil
}

func main() {
	cfg = config.Load()

	// Initialize NorthWind client (authenticate on startup if enabled)
	northWindService := services.NewNorthWindService(cfg.NorthWind, slog.Default())
	if cfg.NorthWind.EnableAuth && cfg.NorthWind.BaseURL != "" {
		if err := northWindService.Authenticate(); err != nil {
			log.Printf("Warning: Failed to authenticate with NorthWind: %v", err)
			log.Println("NorthWind integration will be unavailable until authentication succeeds")
		} else {
			log.Println("Successfully authenticated with NorthWind Bank")
		}
	}

	// Initialize database
	db, err := database.Initialize(cfg)
	if err != nil {
		log.Fatal("Failed to initialize database:", err)
	}

	// Initialize repositories
	userRepo := repositories.NewUserRepository(db)
	refreshTokenRepo := repositories.NewRefreshTokenRepository(db)
	auditLogRepo := repositories.NewAuditLogRepository(db)
	blacklistedTokenRepo := repositories.NewBlacklistedTokenRepository(db)
	accountRepo := repositories.NewAccountRepository(db)
	transactionRepo := repositories.NewTransactionRepository(db)
	transferRepo := repositories.NewTransferRepository(db)
	processingQueueRepo := repositories.NewProcessingQueueRepository(db)
	externalAccountRepo := repositories.NewExternalAccountRepository(db)

	// Initialize services
	auditService := services.NewAuditService(auditLogRepo)
	passwordService := services.NewPasswordService(userRepo, auditService)
	tokenService := services.NewTokenService(&cfg.JWT)

	accountService := services.NewAccountService(
		accountRepo,
		transactionRepo,
		transferRepo,
		userRepo,
		auditLogRepo,
		slog.Default(),
	)

	authService := services.NewAuthService(
		userRepo,
		refreshTokenRepo,
		auditLogRepo,
		blacklistedTokenRepo,
		passwordService,
		tokenService,
		accountService,
		slog.Default(),
	)

	auditLogger := services.NewAuditLogger(slog.Default())
	prometheusMetrics := services.NewPrometheusMetrics()
	circuitBreaker := services.NewCircuitBreaker(services.DefaultCircuitBreakerConfig())

	processingService := services.NewTransactionProcessingService(
		transactionRepo,
		processingQueueRepo,
		accountRepo,
		auditLogger,
		prometheusMetrics,
		circuitBreaker,
		5,
	)

	accountSummaryService := services.NewAccountSummaryService(accountRepo, userRepo)
	accountMetricsService := services.NewAccountMetricsService(accountRepo, transactionRepo, userRepo)
	statementService := services.NewStatementService(accountRepo, transactionRepo, userRepo, accountMetricsService)

	// Customer management services
	customerSearchService := services.NewCustomerSearchService(userRepo)
	customerProfileService := services.NewCustomerProfileService(userRepo, accountRepo, auditService)
	accountAssociationService := services.NewAccountAssociationService(userRepo, accountRepo, auditService, slog.Default())
	customerLogger := services.NewCustomerLogger(slog.Default())

	// External account and transfer services
	externalAccountService := services.NewExternalAccountService(
		externalAccountRepo,
		userRepo,
		northWindService,
		auditService,
		slog.Default(),
	)

	northWindTransferService := services.NewNorthWindTransferService(
		transferRepo,
		accountRepo,
		externalAccountRepo,
		transactionRepo,
		northWindService,
		auditService,
		slog.Default(),
	)

	processingCtx, cancelProcessing := context.WithCancel(context.Background())
	defer cancelProcessing()

	go processingService.StartProcessing(processingCtx)

	e := configureEcho()

	authHandler := handlers.NewAuthHandler(authService)
	adminHandler := handlers.NewAdminHandler(userRepo, auditLogRepo)
	accountHandler := handlers.NewAccountHandler(accountService, auditLogger, prometheusMetrics)
	transactionHandler := handlers.NewTransactionHandler(transactionRepo, accountRepo)
	accountSummaryHandler := handlers.NewAccountSummaryHandler(accountSummaryService, accountMetricsService, statementService)
	devHandler := handlers.NewDevHandler(transactionRepo, accountRepo)
	customerHandler := handlers.NewCustomerHandler(customerSearchService, customerProfileService, accountAssociationService, passwordService, auditService, customerLogger, prometheusMetrics)
	healthCheckHandler := handlers.NewHealthCheckHandler(db)
	docsHandler := handlers.NewDocsHandler()
	externalAccountHandler := handlers.NewExternalAccountHandler(externalAccountService)
	externalTransferHandler := handlers.NewExternalTransferHandler(northWindTransferService)

	api := e.Group("/api/v1")
	tokenSvc := tokenService.(*services.TokenService)
	addAuthEndpoints(api, tokenSvc, blacklistedTokenRepo, authHandler)
	addAccountEndpoints(api, tokenSvc, blacklistedTokenRepo, accountHandler, accountSummaryHandler, transactionHandler, customerHandler)
	addCustomerEndpoints(api, tokenSvc, blacklistedTokenRepo, customerHandler, accountHandler)
	addDevEndpoints(api, tokenSvc, blacklistedTokenRepo, devHandler)
	addAdminEndpoints(api, tokenSvc, blacklistedTokenRepo, adminHandler, accountHandler)
	addHealthCheckEndpoint(api, healthCheckHandler)
	addDocumentationEndpoints(e, docsHandler)
	addExternalAccountEndpoints(api, tokenSvc, blacklistedTokenRepo, externalAccountHandler)
	addExternalTransferEndpoints(api, tokenSvc, blacklistedTokenRepo, externalTransferHandler)
	addDocumentationEndpoints(e, docsHandler)

	go func() {
		if err := e.Start(":" + cfg.Server.Port); err != nil && err != http.ErrServerClosed {
			log.Fatal("Failed to start server:", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt)
	<-quit

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := e.Shutdown(ctx); err != nil {
		log.Fatal("Server forced to shut down:", err)
	}

	log.Println("Server shutdown complete")
}

func configureEcho() *echo.Echo {
	e := echo.New()
	e.HideBanner = true
	// Use our custom validator with business rule validations
	customValidator := validation.GetValidator()
	e.Validator = &CustomValidator{validator: customValidator.GetValidate()}
	e.HTTPErrorHandler = middleware.CustomHTTPErrorHandler

	e.Use(middleware.RequestID())
	e.Use(middleware.PanicRecovery())
	e.Use(echomiddleware.Logger())
	e.Use(middleware.RateLimiter())
	e.Use(middleware.SecurityHeaders())
	e.Use(echomiddleware.CORSWithConfig(echomiddleware.CORSConfig{
		AllowOrigins: cfg.Server.CORSAllowOrigins,
		AllowMethods: []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodOptions},
		AllowHeaders: []string{echo.HeaderOrigin, echo.HeaderContentType, echo.HeaderAccept, echo.HeaderAuthorization, middleware.TraceIDHeader},
	}))
	return e
}

func addAuthEndpoints(api *echo.Group, tokenService *services.TokenService, blacklistedTokenRepo repositories.BlacklistedTokenRepositoryInterface, authHandler *handlers.AuthHandler) {
	authGroup := api.Group("/auth")
	authGroup.POST("/register", authHandler.Register)
	authGroup.POST("/login", authHandler.Login)
	authGroup.POST("/refresh", authHandler.RefreshToken)
	authGroup.POST("/logout", authHandler.Logout, middleware.RequireAuth(tokenService, blacklistedTokenRepo))
}

func addAccountEndpoints(api *echo.Group, tokenService *services.TokenService, blacklistedTokenRepo repositories.BlacklistedTokenRepositoryInterface, accountHandler *handlers.AccountHandler, accountSummaryHandler *handlers.AccountSummaryHandler, transactionHandler *handlers.TransactionHandler, customerHandler *handlers.CustomerHandler) {
	accountGroup := api.Group("/accounts", middleware.RequireAuth(tokenService, blacklistedTokenRepo))
	accountGroup.POST("", accountHandler.CreateAccount)
	accountGroup.GET("", accountHandler.GetUserAccounts)
	accountGroup.GET("/:accountId", accountHandler.GetAccount)
	accountGroup.PATCH("/:accountId/status", accountHandler.UpdateAccountStatus)
	accountGroup.DELETE("/:accountId", accountHandler.CloseAccount)
	accountGroup.POST("/:accountId/transactions", accountHandler.PerformTransaction)
	accountGroup.GET("/:accountId/transactions", transactionHandler.ListTransactions)
	accountGroup.GET("/:accountId/transactions/:id", transactionHandler.GetTransaction)
	accountGroup.POST("/:accountId/transfer", accountHandler.Transfer)

	// Summary endpoints
	accountGroup.GET("/summary", accountSummaryHandler.GetAccountSummary)
	accountGroup.GET("/metrics", accountSummaryHandler.GetAccountMetrics)
	accountGroup.GET("/:accountId/statements", accountSummaryHandler.GetStatement)

	// Account ownership transfer endpoint (admin-only)
	accountGroup.POST("/:accountId/transfer-ownership", customerHandler.TransferAccountOwnership, middleware.RequireAdmin())
}

func addDevEndpoints(api *echo.Group, tokenService *services.TokenService, blacklistedTokenRepo repositories.BlacklistedTokenRepositoryInterface, devHandler *handlers.DevHandler) {
	if !cfg.IsProduction() {
		devGroup := api.Group("/dev", middleware.RequireAuth(tokenService, blacklistedTokenRepo))
		devGroup.POST("/accounts/:accountId/generate-test-data", devHandler.GenerateTestData)
		devGroup.DELETE("/accounts/:accountId/test-data", devHandler.ClearTestData)
	}
}

func addAdminEndpoints(api *echo.Group, tokenService *services.TokenService, blacklistedTokenRepo repositories.BlacklistedTokenRepositoryInterface, adminHandler *handlers.AdminHandler, accountHandler *handlers.AccountHandler) {
	adminGroup := api.Group("/admin", middleware.RequireAuth(tokenService, blacklistedTokenRepo), middleware.RequireAdmin())
	addAdminUserManagementEndpoints(adminGroup, adminHandler)
	addAdminAccountManagementEndpoints(adminGroup, accountHandler)
}

func addAdminAccountManagementEndpoints(adminGroup *echo.Group, accountHandler *handlers.AccountHandler) {
	adminGroup.GET("/accounts", accountHandler.GetAllAccounts)
	adminGroup.GET("/accounts/:accountId", accountHandler.GetAccountByIDAdmin)
	adminGroup.GET("/users/:userId/accounts", accountHandler.GetUserAccountsAdmin)
}

func addAdminUserManagementEndpoints(adminGroup *echo.Group, adminHandler *handlers.AdminHandler) {
	adminGroup.POST("/users/:userId/unlock", adminHandler.UnlockUser)
	adminGroup.GET("/users", adminHandler.ListUsers)
	adminGroup.GET("/users/:userId", adminHandler.GetUserByID)
	adminGroup.DELETE("/users/:userId", adminHandler.DeleteUser)
}

func addCustomerEndpoints(api *echo.Group, tokenService *services.TokenService, blacklistedTokenRepo repositories.BlacklistedTokenRepositoryInterface, customerHandler *handlers.CustomerHandler, accountHandler *handlers.AccountHandler) {
	// Admin-only customer management endpoints
	adminCustomerGroup := api.Group("/customers", middleware.RequireAuth(tokenService, blacklistedTokenRepo), middleware.RequireAdmin())
	adminCustomerGroup.GET("/search", customerHandler.SearchCustomers)
	adminCustomerGroup.POST("", customerHandler.CreateCustomer)
	adminCustomerGroup.GET("/:id", customerHandler.GetCustomerProfile)
	adminCustomerGroup.PUT("/:id", customerHandler.UpdateCustomerProfile)
	adminCustomerGroup.DELETE("/:id", customerHandler.DeleteCustomer)
	adminCustomerGroup.GET("/:id/accounts", customerHandler.GetCustomerAccounts)
	adminCustomerGroup.POST("/:id/accounts", customerHandler.CreateAccountForCustomer)
	adminCustomerGroup.GET("/:id/activity", customerHandler.GetCustomerActivity)
	adminCustomerGroup.PUT("/:id/password/reset", customerHandler.ResetCustomerPassword)

	// Self-service customer endpoints (authenticated users)
	selfServiceGroup := api.Group("/customers/me", middleware.RequireAuth(tokenService, blacklistedTokenRepo))
	selfServiceGroup.GET("", customerHandler.GetMyProfile)
	selfServiceGroup.PUT("/email", customerHandler.UpdateMyEmail)
	selfServiceGroup.GET("/accounts", customerHandler.GetMyAccounts)
	selfServiceGroup.GET("/transfers", accountHandler.GetTransferHistory)
	selfServiceGroup.GET("/activity", customerHandler.GetMyActivity)
	selfServiceGroup.PUT("/password", customerHandler.UpdateMyPassword)
}

// addHealthCheckEndpoint registers the health check endpoint
func addHealthCheckEndpoint(api *echo.Group, healthCheckHandler *handlers.HealthCheckHandler) {
	api.GET("/health", healthCheckHandler.HealthCheck)
}

// addExternalAccountEndpoints registers external account management endpoints
func addExternalAccountEndpoints(api *echo.Group, tokenService *services.TokenService, blacklistedTokenRepo repositories.BlacklistedTokenRepositoryInterface, externalAccountHandler *handlers.ExternalAccountHandler) {
	externalAccountGroup := api.Group("/external-accounts", middleware.RequireAuth(tokenService, blacklistedTokenRepo))
	externalAccountGroup.POST("", externalAccountHandler.RegisterExternalAccount)
	externalAccountGroup.GET("", externalAccountHandler.GetExternalAccounts)
	externalAccountGroup.GET("/:accountId", externalAccountHandler.GetExternalAccount)
	externalAccountGroup.DELETE("/:accountId", externalAccountHandler.DeleteExternalAccount)
}

// addExternalTransferEndpoints registers external transfer endpoints
func addExternalTransferEndpoints(api *echo.Group, tokenService *services.TokenService, blacklistedTokenRepo repositories.BlacklistedTokenRepositoryInterface, externalTransferHandler *handlers.ExternalTransferHandler) {
	externalTransferGroup := api.Group("/external-transfers", middleware.RequireAuth(tokenService, blacklistedTokenRepo))
	externalTransferGroup.POST("", externalTransferHandler.InitiateExternalTransfer)
	externalTransferGroup.GET("/:transferId/status", externalTransferHandler.GetTransferStatus)
}

// addDocumentationEndpoints registers API documentation routes
// These endpoints are public (no authentication required) to allow developers
// to explore the API before registering
func addDocumentationEndpoints(e *echo.Echo, docsHandler *handlers.DocsHandler) {
	e.GET("/docs", docsHandler.ServeScalarUI)
	e.GET("/docs/swagger.json", docsHandler.ServeOAS3JSON)
}
