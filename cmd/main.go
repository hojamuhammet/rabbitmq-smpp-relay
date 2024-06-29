package main

import (
	"log"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"

	"rabbitmq-smpp-relay/internal/config"
	smpp "rabbitmq-smpp-relay/internal/infrastructure"
	"rabbitmq-smpp-relay/pkg/logger"

	"github.com/streadway/amqp"
)

func main() {
	// Initialize the logger
	env := os.Getenv("ENV")
	loggers, err := logger.SetupLogger(env)
	if err != nil {
		log.Fatalf("Failed to set up logger: %v", err)
	}

	cfg := config.LoadConfig()

	smppClient, err := smpp.NewSMPPClient(cfg, loggers)
	if err != nil {
		loggers.ErrorLogger.Error("Failed to create SMPP client", "error", err)
		return
	}

	conn, err := amqp.Dial(cfg.Rabbitmq.URL)
	if err != nil {
		loggers.ErrorLogger.Error("Failed to connect to RabbitMQ", "error", err)
		return
	}
	defer conn.Close()

	ch, err := conn.Channel()
	if err != nil {
		loggers.ErrorLogger.Error("Failed to open a channel", "error", err)
		return
	}
	defer ch.Close()

	_, err = ch.QueueDeclare(
		cfg.Rabbitmq.Queue,
		true,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		loggers.ErrorLogger.Error("Failed to declare a queue", "error", err)
		return
	}

	msgs, err := ch.Consume(
		cfg.Rabbitmq.Queue,
		"",
		true,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		loggers.ErrorLogger.Error("Failed to register a consumer", "error", err)
		return
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	var wg sync.WaitGroup

	go func() {
		<-sigChan
		loggers.InfoLogger.Info("Shutting down gracefully...")
		conn.Close()
		wg.Wait()
		os.Exit(0)
	}()

	loggers.InfoLogger.Info("Waiting for messages...")
	for msg := range msgs {
		wg.Add(1)
		go func(msg amqp.Delivery) {
			defer wg.Done()

			loggers.InfoLogger.Info("Received a message", "body", string(msg.Body))
			message := string(msg.Body)
			if !strings.HasPrefix(message, "src=") {
				loggers.ErrorLogger.Error("Invalid message format", "message", message)
				return
			}

			parts := strings.SplitN(message, ", dst=", 2)
			if len(parts) != 2 {
				loggers.ErrorLogger.Error("Invalid message format", "message", message)
				return
			}
			src := strings.TrimPrefix(parts[0], "src=")

			remaining := strings.SplitN(parts[1], ", txt=", 2)
			if len(remaining) != 2 {
				loggers.ErrorLogger.Error("Invalid message format", "message", message)
				return
			}

			dst := remaining[0]
			txt := remaining[1]

			if err := smppClient.SendSMS(src, dst, txt); err != nil {
				loggers.ErrorLogger.Error("Failed to send SMS", "error", err)
			} else {
				loggers.InfoLogger.Info("SMS sent successfully", "src", src, "dst", dst, "txt", txt)
			}
		}(msg)
	}

	wg.Wait()
}
