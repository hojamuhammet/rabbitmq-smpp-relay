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

	smppClient, err := smpp.NewSMPPClient(cfg, loggers, 100)
	if err != nil {
		loggers.ErrorLogger.Error("Failed to create SMPP client", "error", err)
		return
	}

	rabbitMQ, err := rabbitmq.NewRabbitMQ(cfg, loggers, 200)
	if err != nil {
		loggers.ErrorLogger.Error("Failed to initialize RabbitMQ", "error", err)
		return
	}

	msgService := service.NewMessageService(smppClient, loggers)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	done := make(chan struct{})
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		rabbitMQ.ConsumeMessages(msgService.HandleMessage, done)
	}()

	<-sigChan
	loggers.InfoLogger.Info("Received shutdown signal, shutting down gracefully...")

	close(done)

	rabbitMQ.Close()

	smppClient.Close()

	loggers.InfoLogger.Info("Server shut down gracefully.")

	wg.Wait()
}
