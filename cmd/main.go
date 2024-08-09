package main

import (
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"rabbitmq-smpp-relay/internal/config"
	"rabbitmq-smpp-relay/internal/infrastructure/rabbitmq"
	"rabbitmq-smpp-relay/internal/infrastructure/smpp"
	"rabbitmq-smpp-relay/internal/service"
	"rabbitmq-smpp-relay/pkg/logger"
)

func main() {
	cfg := config.LoadConfig()

	loggers, err := logger.SetupLogger(cfg.Env)
	if err != nil {
		log.Fatalf("Failed to set up logger: %v", err)
	}

	loggers.InfoLogger.Info("Starting the server...")

	smppClient, err := smpp.NewSMPPClient(cfg, loggers)
	if err != nil {
		loggers.ErrorLogger.Error("Failed to create SMPP client", "error", err)
		return
	}

	rabbitMQ, err := rabbitmq.NewRabbitMQ(cfg, loggers)
	if err != nil {
		loggers.ErrorLogger.Error("Failed to initialize RabbitMQ", "error", err)
		return
	}

	msgService := service.NewMessageService(smppClient, loggers)

	// Channel to handle shutdown signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Channel to signal when to stop processing
	done := make(chan struct{})
	var wg sync.WaitGroup

	// Start consuming messages
	wg.Add(1)
	go func() {
		defer wg.Done()
		rabbitMQ.ConsumeMessages(msgService.HandleMessage, done)
	}()

	// Wait for shutdown signal
	go func() {
		<-sigChan
		loggers.InfoLogger.Info("Received shutdown signal, shutting down gracefully...")

		// Signal all goroutines to stop
		close(done)

		// Close RabbitMQ connection and channel
		rabbitMQ.Close()

		// Close SMPP client
		smppClient.Transmitter.Close()

		loggers.InfoLogger.Info("Server shut down gracefully.")
	}()

	// Wait for all goroutines to complete
	wg.Wait()
}
