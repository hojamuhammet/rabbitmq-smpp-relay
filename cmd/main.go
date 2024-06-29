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

	"github.com/streadway/amqp"
)

func main() {
	cfg := config.LoadConfig()

	smppClient, err := smpp.NewSMPPClient(cfg)
	if err != nil {
		log.Fatalf("Failed to create SMPP client: %v", err)
	}

	conn, err := amqp.Dial(cfg.Rabbitmq.URL)
	if err != nil {
		log.Fatalf("Failed to connect to RabbitMQ: %v", err)
	}
	defer conn.Close()

	ch, err := conn.Channel()
	if err != nil {
		log.Fatalf("Failed to open a channel: %v", err)
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
		log.Fatalf("Failed to declare a queue: %v", err)
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
		log.Fatalf("Failed to register a consumer: %v", err)
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	var wg sync.WaitGroup

	go func() {
		<-sigChan
		log.Println("Shutting down gracefully...")
		conn.Close()
		wg.Wait()
		os.Exit(0)
	}()

	log.Println("Waiting for messages...")
	for msg := range msgs {
		wg.Add(1)
		go func(msg amqp.Delivery) {
			defer wg.Done()

			log.Printf("Received a message: %s", msg.Body)
			message := string(msg.Body)
			if !strings.HasPrefix(message, "src=") {
				log.Printf("Invalid message format: %s", message)
				return
			}

			parts := strings.SplitN(message, ", dst=", 2)
			if len(parts) != 2 {
				log.Printf("Invalid message format: %s", message)
				return
			}
			src := strings.TrimPrefix(parts[0], "src=")

			remaining := strings.SplitN(parts[1], ", txt=", 2)
			if len(remaining) != 2 {
				log.Printf("Invalid message format: %s", message)
				return
			}

			dst := remaining[0]
			txt := remaining[1]

			if err := smppClient.SendSMS(src, dst, txt); err != nil {
				log.Printf("Failed to send SMS: %v", err)
			}
		}(msg)
	}

	wg.Wait()
}
