package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	appemployee "github.com/navyaraksha/imogi/internal/application/employee"
	appfilebatch "github.com/navyaraksha/imogi/internal/application/filebatch"
	appidentity "github.com/navyaraksha/imogi/internal/application/identity"
	apporganization "github.com/navyaraksha/imogi/internal/application/organization"
	apppayroll "github.com/navyaraksha/imogi/internal/application/payroll"
	infrastructureobjectstorage "github.com/navyaraksha/imogi/internal/infrastructure/objectstorage"
	"github.com/navyaraksha/imogi/internal/infrastructure/postgres"
	"github.com/navyaraksha/imogi/internal/platform/clock"
	"github.com/navyaraksha/imogi/internal/platform/config"
	platformidentity "github.com/navyaraksha/imogi/internal/platform/identity"
	"github.com/navyaraksha/imogi/internal/platform/security"
	"github.com/navyaraksha/imogi/internal/restapi"
)

func main() {
	if err := run(context.Background()); err != nil {
		// Startup errors can contain database connection details. Keep them out
		// of structured logs; deployment diagnostics should come from a
		// controlled, secret-aware error sink.
		slog.Error("server stopped")
		os.Exit(1)
	}
}

func run(parent context.Context) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	poolConfig, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		return err
	}
	poolConfig.MaxConns = cfg.DatabaseMaxConns
	poolConfig.MinConns = cfg.DatabaseMinConns
	pool, err := pgxpool.NewWithConfig(parent, poolConfig)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := pool.Ping(parent); err != nil {
		return err
	}

	protector, err := security.NewAESGCMProtector(cfg.EncryptionKey, cfg.LookupKey)
	if err != nil {
		return err
	}
	authorizer := security.ContextAuthorizer{}
	systemClock := clock.System{}

	identityRepository, err := postgres.NewIdentityRepository(pool)
	if err != nil {
		return err
	}
	accessResolver, err := appidentity.NewAccessService(identityRepository, cfg.BootstrapAdminEmails)
	if err != nil {
		return err
	}
	googleAuthenticator, err := platformidentity.NewGoogleAuthenticator(parent, cfg.GoogleClientID, cfg.GoogleHostedDomain, accessResolver, http.DefaultClient)
	if err != nil {
		return err
	}
	sessionService, err := appidentity.NewSessionService(identityRepository, accessResolver, systemClock, cfg.SessionTTL)
	if err != nil {
		return err
	}
	requestAuthenticator, err := platformidentity.NewRequestAuthenticator(googleAuthenticator, sessionService)
	if err != nil {
		return err
	}

	employeeRepository, err := postgres.NewRepository(pool, protector)
	if err != nil {
		return err
	}
	employeeService, err := appemployee.NewService(employeeRepository, authorizer, systemClock)
	if err != nil {
		return err
	}
	organizationRepository, err := postgres.NewOrganizationRepository(pool)
	if err != nil {
		return err
	}
	organizationService, err := apporganization.NewService(organizationRepository, authorizer, systemClock)
	if err != nil {
		return err
	}
	identityProvisioningService, err := appidentity.NewProvisioningService(identityRepository, authorizer, systemClock)
	if err != nil {
		return err
	}
	payrollRepository, err := postgres.NewPayrollRepository(pool)
	if err != nil {
		return err
	}
	payrollService, err := apppayroll.NewService(payrollRepository, authorizer, systemClock)
	if err != nil {
		return err
	}
	fileStorage, err := infrastructureobjectstorage.New(infrastructureobjectstorage.Config{
		Driver: cfg.ObjectStorageDriver, Endpoint: cfg.ObjectStorageEndpoint, Region: cfg.ObjectStorageRegion,
		Bucket: cfg.ObjectStorageBucket, AccessKey: cfg.ObjectStorageAccessKey, SecretKey: cfg.ObjectStorageSecretKey,
		RootDirectory: cfg.FileStorageRoot, Secure: cfg.ObjectStorageSecure,
	})
	if err != nil {
		return err
	}
	fileBatchRepository, err := postgres.NewFileBatchRepository(pool)
	if err != nil {
		return err
	}
	fileBatchService, err := appfilebatch.NewService(fileBatchRepository, fileStorage, authorizer, systemClock, cfg.ObjectStoragePresignTTL, cfg.FileMaxUploadBytes, cfg.JobMaxAttempts)
	if err != nil {
		return err
	}
	apiHandler, err := restapi.NewHandlerWithOrganization(employeeService, organizationService, identityProvisioningService, sessionService, payrollService, fileBatchService, cfg.SessionCookieSecure, systemClock)
	if err != nil {
		return err
	}
	router, err := restapi.NewRouterWithOptions(apiHandler, requestAuthenticator, restapi.RouterOptions{
		AllowedOrigins:      cfg.HTTPAllowedOrigins,
		SessionCookieSecure: cfg.SessionCookieSecure,
	})
	if err != nil {
		return err
	}

	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	serverErrors := make(chan error, 1)
	go func() {
		logger.Info("http server listening", "addr", cfg.HTTPAddr)
		serverErrors <- server.ListenAndServe()
	}()

	shutdownContext, stop := signal.NotifyContext(parent, syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	select {
	case err := <-serverErrors:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-shutdownContext.Done():
		ctx, cancel := context.WithTimeout(context.Background(), cfg.HTTPShutdownTimeout)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			return err
		}
		return nil
	}
}
