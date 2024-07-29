package rabbitmq

import (
	"sync"
	"time"

	"rabbitmq-smpp-relay/internal/config"
	"rabbitmq-smpp-relay/pkg/logger"

	"github.com/streadway/amqp"
)

const (
	ReconnectDelay = 5 * time.Second
)

type RabbitMQ struct {
	conn    *amqp.Connection
	ch      *amqp.Channel
	cfg     *config.Config
	log     *logger.Loggers
	mu      sync.Mutex
	wg      sync.WaitGroup
	done    chan struct{}
	handler func(amqp.Delivery)
}

func NewRabbitMQ(cfg *config.Config, loggers *logger.Loggers) (*RabbitMQ, error) {
	rmq := &RabbitMQ{
		cfg:  cfg,
		log:  loggers,
		done: make(chan struct{}),
	}
	if err := rmq.connect(); err != nil {
		return nil, err
	}
	go rmq.handleReconnect()
	return rmq, nil
}

func (r *RabbitMQ) connect() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	var err error
	r.conn, err = amqp.Dial(r.cfg.Rabbitmq.URL)
	if err != nil {
		r.log.ErrorLogger.Error("Failed to connect to RabbitMQ", "error", err)
		return err
	}

	r.ch, err = r.conn.Channel()
	if err != nil {
		r.conn.Close()
		r.log.ErrorLogger.Error("Failed to create RabbitMQ channel", "error", err)
		return err
	}

	_, err = r.ch.QueueDeclare(
		r.cfg.Rabbitmq.Queue,
		true,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		r.conn.Close()
		r.ch.Close()
		r.log.ErrorLogger.Error("Failed to declare RabbitMQ queue", "error", err)
		return err
	}

	err = r.ch.QueueBind(
		r.cfg.Rabbitmq.Queue,
		"",
		r.cfg.Rabbitmq.Exchange,
		false,
		nil,
	)
	if err != nil {
		r.conn.Close()
		r.ch.Close()
		r.log.ErrorLogger.Error("Failed to bind RabbitMQ queue", "error", err)
		return err
	}

	r.log.InfoLogger.Info("RabbitMQ connection established.")
	return nil
}

func (r *RabbitMQ) handleReconnect() {
	for {
		closeErrCh := make(chan *amqp.Error)
		r.conn.NotifyClose(closeErrCh)

		select {
		case err := <-closeErrCh:
			if err != nil {
				r.log.ErrorLogger.Error("RabbitMQ connection closed", "error", err)
				r.reconnect()
			}
		case <-r.done:
			return
		}
	}
}

func (r *RabbitMQ) reconnect() {
	for {
		select {
		case <-r.done:
			return
		default:
			r.log.InfoLogger.Info("Attempting to reconnect to RabbitMQ...")
			if err := r.connect(); err == nil {
				r.log.InfoLogger.Info("Successfully reconnected to RabbitMQ.")
				go r.consumeMessages() // Only start consuming messages after successful reconnection
				return
			}
			time.Sleep(ReconnectDelay)
		}
	}
}

func (r *RabbitMQ) ConsumeMessages(handler func(amqp.Delivery), done <-chan struct{}) {
	r.handler = handler
	go r.consumeMessages()
	<-done
	r.Close()
}

func (r *RabbitMQ) consumeMessages() {
	r.wg.Add(1)
	defer r.wg.Done()

	for {
		msgs, err := r.ch.Consume(
			r.cfg.Rabbitmq.Queue,
			"",
			true,
			false,
			false,
			false,
			nil,
		)
		if err != nil {
			r.log.ErrorLogger.Error("Failed to start consuming messages", "error", err)
			return
		}

		for {
			select {
			case msg, ok := <-msgs:
				if !ok {
					r.log.InfoLogger.Info("Channel closed, waiting for reconnect")
					return
				}
				r.wg.Add(1)
				go func(msg amqp.Delivery) {
					defer r.wg.Done()
					r.handler(msg)
				}(msg)
			case <-r.done:
				r.log.InfoLogger.Info("ConsumeMessages received done signal")
				return
			}
		}
	}
}

func (r *RabbitMQ) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()

	select {
	case <-r.done:
	default:
		close(r.done)
	}

	r.wg.Wait()

	if r.ch != nil {
		r.ch.Close()
	}
	if r.conn != nil {
		r.conn.Close()
	}

	r.log.InfoLogger.Info("RabbitMQ connection closed.")
}