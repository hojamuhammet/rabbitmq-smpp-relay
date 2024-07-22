package main

import (
	"encoding/json"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"rabbitmq-smpp-relay/internal/config"
	smpp "rabbitmq-smpp-relay/internal/infrastructure"
	"rabbitmq-smpp-relay/pkg/logger"
	"rabbitmq-smpp-relay/pkg/utils"

	"github.com/streadway/amqp"
)

type Message struct {
	Src string `json:"src"`
	Dst string `json:"dst"`
	Msg string `json:"msg"`
}

func main() {
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

	conn, ch, err := connectRabbitMQ(cfg)
	if err != nil {
		loggers.ErrorLogger.Error("Failed to connect to RabbitMQ", "error", err)
		return
	}
	defer conn.Close()
	defer ch.Close()

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

	go monitorNetwork(loggers, cfg, &conn, &ch, &wg, smppClient)

	loggers.InfoLogger.Info("Waiting for messages...")
	consumeMessages(ch, smppClient, loggers, &wg, cfg)

	wg.Wait()
}

func connectRabbitMQ(cfg *config.Config) (*amqp.Connection, *amqp.Channel, error) {
	conn, err := amqp.Dial(cfg.Rabbitmq.URL)
	if err != nil {
		return nil, nil, err
	}

	ch, err := conn.Channel()
	if err != nil {
		conn.Close()
		return nil, nil, err
	}

	_, err = ch.QueueDeclare(
		cfg.Rabbitmq.Queue,
		true,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		conn.Close()
		ch.Close()
		return nil, nil, err
	}

	err = ch.QueueBind(
		cfg.Rabbitmq.Queue,
		"",          // routing key
		"sms_reply", // exchange
		false,
		nil)
	if err != nil {
		conn.Close()
		ch.Close()
		return nil, nil, err
	}

	return conn, ch, nil
}

func consumeMessages(ch *amqp.Channel, smppClient *smpp.SMPPClient, loggers *logger.Loggers, wg *sync.WaitGroup, cfg *config.Config) {
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

	for msg := range msgs {
		wg.Add(1)
		go func(msg amqp.Delivery) {
			defer wg.Done()

			rawBody := string(msg.Body)
			loggers.InfoLogger.Info("Received a raw message", "body", rawBody)

			var message Message
			if err := json.Unmarshal(msg.Body, &message); err != nil {
				loggers.ErrorLogger.Error("Invalid message format", "error", err, "body", rawBody)
				return
			}

			loggers.InfoLogger.Info("Parsed message", "src", message.Src, "dst", message.Dst, "msg", message.Msg)

			if message.Src == "" || message.Dst == "" || message.Msg == "" {
				loggers.ErrorLogger.Error("Message fields cannot be empty", "src", message.Src, "dst", message.Dst, "msg", message.Msg)
				return
			}

			go func() {
				if err := smppClient.SendSMS(message.Src, message.Dst, message.Msg); err != nil {
					loggers.ErrorLogger.Error("Failed to send SMS", "error", err)
				} else {
					loggers.InfoLogger.Info("SMS sent successfully", "src", message.Src, "dst", message.Dst, "msg", message.Msg)
				}
			}()
		}(msg)
	}
}

func monitorNetwork(loggers *logger.Loggers, cfg *config.Config, conn **amqp.Connection, ch **amqp.Channel, wg *sync.WaitGroup, smppClient *smpp.SMPPClient) {
	wasNetworkAvailable := true
	for {
		isNetworkAvailable := utils.IsNetworkAvailable()
		if isNetworkAvailable && !wasNetworkAvailable {
			loggers.InfoLogger.Info("Network connection restored")
			newConn, newCh, err := connectRabbitMQ(cfg)
			if err != nil {
				loggers.ErrorLogger.Error("Failed to reconnect to RabbitMQ", "error", err)
			} else {
				loggers.InfoLogger.Info("Reconnected to RabbitMQ")
				*conn = newConn
				*ch = newCh
				go consumeMessages(newCh, smppClient, loggers, wg, cfg)
			}
		} else if !isNetworkAvailable && wasNetworkAvailable {
			loggers.ErrorLogger.Error("Network connection lost")
		} else {
			loggers.InfoLogger.Debug("Network status unchanged", "isNetworkAvailable", isNetworkAvailable)
		}
		wasNetworkAvailable = isNetworkAvailable
		time.Sleep(5 * time.Second)
	}
}
