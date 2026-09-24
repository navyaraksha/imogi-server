package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"

	appfilebatch "github.com/navyaraksha/imogi/internal/application/filebatch"
	appjob "github.com/navyaraksha/imogi/internal/application/job"
	"github.com/navyaraksha/imogi/internal/infrastructure/fileparser"
	"github.com/navyaraksha/imogi/internal/infrastructure/objectstorage"
	"github.com/navyaraksha/imogi/internal/infrastructure/postgres"
	"github.com/navyaraksha/imogi/internal/platform/clock"
	"github.com/navyaraksha/imogi/internal/platform/config"
	"github.com/navyaraksha/imogi/internal/platform/security"
)

func main() {
	if err := run(context.Background()); err != nil && !errors.Is(err, context.Canceled) {
		slog.Error("worker stopped", "error", err)
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

	storage, err := objectstorage.New(objectstorage.Config{
		Driver: cfg.ObjectStorageDriver, Endpoint: cfg.ObjectStorageEndpoint, Region: cfg.ObjectStorageRegion,
		Bucket: cfg.ObjectStorageBucket, AccessKey: cfg.ObjectStorageAccessKey, SecretKey: cfg.ObjectStorageSecretKey,
		RootDirectory: cfg.FileStorageRoot, Secure: cfg.ObjectStorageSecure,
	})
	if err != nil {
		return err
	}
	fileRepository, err := postgres.NewFileBatchRepository(pool)
	if err != nil {
		return err
	}
	jobRepository, err := postgres.NewJobRepository(pool)
	if err != nil {
		return err
	}
	parser, err := fileparser.New(cfg.FileMaxRows)
	if err != nil {
		return err
	}
	validator, err := appfilebatch.NewValidator(fileRepository, storage, parser, clock.System{}, cfg.FileMaxRows)
	if err != nil {
		return err
	}
	protector, err := security.NewAESGCMProtector(cfg.EncryptionKey, cfg.LookupKey)
	if err != nil {
		return err
	}
	employeeRepository, err := postgres.NewRepository(pool, protector)
	if err != nil {
		return err
	}
	validator.SetIdentityMatcher(employeeRepository)
	payrollWriter, err := postgres.NewPayrollImportWriter(pool, clock.System{})
	if err != nil {
		return err
	}
	committer, err := appfilebatch.NewCommitter(fileRepository, employeeRepository, storage, parser, clock.System{}, payrollWriter)
	if err != nil {
		return err
	}
	registry := appjob.NewRegistry()
	for _, operation := range []string{"employee_master", "employment_history", "assignment_history", "tax_profile_history", "payroll_ledger"} {
		if err := registry.Register(operation+".validate", validator); err != nil {
			return err
		}
	}
	if err := registry.Register("employee_master.commit", committer); err != nil {
		return err
	}
	if err := registry.Register("payroll_ledger.commit", committer); err != nil {
		return err
	}

	workerContext, stop := signal.NotifyContext(parent, syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "unknown-host"
	}
	workerErrors := make(chan error, cfg.JobWorkerConcurrency)
	var waitGroup sync.WaitGroup
	for index := 0; index < cfg.JobWorkerConcurrency; index++ {
		worker, err := appjob.NewWorker(jobRepository, registry, clock.System{}, appjob.WorkerConfig{
			WorkerID: fmt.Sprintf("%s-%d-%d", hostname, os.Getpid(), index), QueueNames: []string{"file-validation", "file-commit"},
			LeaseDuration: cfg.JobLeaseDuration, PollInterval: cfg.JobPollInterval,
		})
		if err != nil {
			return err
		}
		waitGroup.Add(1)
		go func(index int, worker *appjob.Worker) {
			defer waitGroup.Done()
			logger.Info("job worker started", "worker_index", index, "concurrency", cfg.JobWorkerConcurrency, "pid", strconv.Itoa(os.Getpid()))
			if err := worker.Run(workerContext); err != nil && !errors.Is(err, context.Canceled) {
				workerErrors <- err
				stop()
			}
		}(index, worker)
	}

	waitDone := make(chan struct{})
	go func() { waitGroup.Wait(); close(waitDone) }()
	select {
	case err := <-workerErrors:
		return err
	case <-waitDone:
		return nil
	case <-workerContext.Done():
		<-waitDone
		return nil
	}
}
