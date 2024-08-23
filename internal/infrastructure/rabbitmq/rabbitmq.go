package rabbitmq

import (
	"sync"
	"time"

	"rabbitmq-smpp-relay/internal/config"
	"rabbitmq-smpp-relay/pkg/logger"

	"github.com/streadway/amqp"
)

const ReconnectDelay = 5 * time.Second

type RabbitMQ struct {
	conn            *amqp.Connection
	ch              *amqp.Channel
	cfg             *config.Config
	log             *logger.Loggers
	mu              sync.Mutex
	done            chan struct{}
	handler         func(amqp.Delivery)
	reconnecting    bool
	isShuttingDown  bool
	notifyConnClose chan *amqp.Error
	notifyChanClose chan *amqp.Error
	workerPool      chan struct{}
}

func NewRabbitMQ(cfg *config.Config, loggers *logger.Loggers, maxWorkers int) (*RabbitMQ, error) {
	rmq := &RabbitMQ{
		cfg:        cfg,
		log:        loggers,
		done:       make(chan struct{}),
		workerPool: make(chan struct{}, maxWorkers),
	}
	if err := rmq.run(); err != nil {
		return nil, err
	}
	go rmq.monitorConnection()
	return rmq, nil
}

func (r *RabbitMQ) run() error {
	for {
		if err := r.connect(); err != nil {
			r.log.ErrorLogger.Error("Failed to connect to RabbitMQ. Retrying...", "error", err)
			time.Sleep(ReconnectDelay)
		} else {
			return nil
		}
	}
}

func (r *RabbitMQ) connect() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.cleanupConnection()

	var err error
	r.conn, err = amqp.Dial(r.cfg.Rabbitmq.URL)
	if err != nil {
		r.log.ErrorLogger.Error("Failed to connect to RabbitMQ", "error", err)
		return err
	}

	r.ch, err = r.conn.Channel()
	if err != nil {
		r.cleanupConnection()
		r.log.ErrorLogger.Error("Failed to open a channel", "error", err)
		return err
	}

	if err := r.setupChannel(); err != nil {
		r.cleanupConnection()
		r.log.ErrorLogger.Error("Failed to set up RabbitMQ channel", "error", err)
		return err
	}

	r.resetNotifyChannels()

	r.log.InfoLogger.Info("RabbitMQ connection and channel successfully established.")
	return nil
}

func (r *RabbitMQ) setupChannel() error {
	if err := r.ch.ExchangeDeclare(
		r.cfg.Rabbitmq.Exchange,
		"direct",
		true,
		false,
		false,
		false,
		nil,
	); err != nil {
		return err
	}

	if _, err := r.ch.QueueDeclare(
		r.cfg.Rabbitmq.Queue,
		true,
		false,
		false,
		false,
		nil,
	); err != nil {
		return err
	}

	if err := r.ch.QueueBind(
		r.cfg.Rabbitmq.Queue,
		r.cfg.Rabbitmq.Routing_key,
		r.cfg.Rabbitmq.Exchange,
		false,
		nil,
	); err != nil {
		return err
	}

	r.log.InfoLogger.Info("RabbitMQ queue, exchange, and binding successfully established.")
	return nil
}

func (r *RabbitMQ) resetNotifyChannels() {
	r.notifyConnClose = make(chan *amqp.Error)
	r.notifyChanClose = make(chan *amqp.Error)
	r.conn.NotifyClose(r.notifyConnClose)
	r.ch.NotifyClose(r.notifyChanClose)
}

func (r *RabbitMQ) monitorConnection() {
	for {
		select {
		case err := <-r.notifyConnClose:
			if err != nil && !r.isShuttingDown {
				r.log.ErrorLogger.Error("RabbitMQ connection closed", "error", err)
				r.reconnect()
			}
		case err := <-r.notifyChanClose:
			if err != nil && !r.isShuttingDown {
				r.log.ErrorLogger.Error("RabbitMQ channel closed", "error", err)
				r.reconnect()
			}
		case <-r.done:
			return
		}
	}
}

func (r *RabbitMQ) reconnect() {
	r.mu.Lock()
	if r.reconnecting {
		r.mu.Unlock()
		return
	}
	r.reconnecting = true
	r.mu.Unlock()

	defer func() {
		r.mu.Lock()
		r.reconnecting = false
		r.mu.Unlock()
	}()

	for {
		if r.isShuttingDown {
			return
		}

		r.log.InfoLogger.Info("Attempting to reconnect to RabbitMQ...")

		if err := r.connect(); err == nil {
			r.log.InfoLogger.Info("Successfully reconnected to RabbitMQ.")
			r.consumeMessages()
			r.monitorConnection()
			return
		}

		r.log.ErrorLogger.Error("Reconnection attempt failed, retrying...")
		time.Sleep(ReconnectDelay)
	}
}

func (r *RabbitMQ) ConsumeMessages(handler func(amqp.Delivery), done <-chan struct{}) {
	r.handler = handler

	r.consumeMessages()
	<-done
	r.Close()
}

func (r *RabbitMQ) consumeMessages() {
	r.mu.Lock()
	defer r.mu.Unlock()

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
		r.reconnect()
		return
	}

	go func() {
		for msg := range msgs {
			r.workerPool <- struct{}{}
			go func(msg amqp.Delivery) {
				defer func() { <-r.workerPool }()
				r.handler(msg)
			}(msg)
		}

		r.log.ErrorLogger.Error("Message channel closed, attempting to reconnect")
		r.reconnect()
	}()
}

func (r *RabbitMQ) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.isShuttingDown = true

	select {
	case <-r.done:
	default:
		close(r.done)
	}

	r.cleanupConnection()
	r.log.InfoLogger.Info("RabbitMQ connection and channel closed")
}

func (r *RabbitMQ) cleanupConnection() {
	if r.ch != nil {
		r.ch.Close()
		r.ch = nil
	}
	if r.conn != nil {
		r.conn.Close()
		r.conn = nil
	}
}
