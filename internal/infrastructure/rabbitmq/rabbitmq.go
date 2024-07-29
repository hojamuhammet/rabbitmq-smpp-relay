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
	conn *amqp.Connection
	ch   *amqp.Channel
	cfg  *config.Config
	log  *logger.Loggers
	mu   sync.Mutex
	wg   sync.WaitGroup
	done chan struct{}
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
	loggers.InfoLogger.Info("RabbitMQ connection established.")
	return rmq, nil
}

func (r *RabbitMQ) connect() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	var err error
	r.conn, err = amqp.Dial(r.cfg.Rabbitmq.URL)
	if err == nil {
		r.ch, err = r.conn.Channel()
		if err == nil {
			_, err = r.ch.QueueDeclare(
				r.cfg.Rabbitmq.Queue,
				true,
				false,
				false,
				false,
				nil,
			)
			if err == nil {
				err = r.ch.QueueBind(
					r.cfg.Rabbitmq.Queue,
					"",
					r.cfg.Rabbitmq.Exchange,
					false,
					nil,
				)
				if err == nil {
					r.log.InfoLogger.Info("RabbitMQ channel and queue declared.")
					return nil
				}
			}
		}
	}
	r.log.ErrorLogger.Error("Failed to connect to RabbitMQ", "error", err)
	return err
}

func (r *RabbitMQ) ConsumeMessages(handler func(amqp.Delivery), done <-chan struct{}) {
	for {
		select {
		case <-done:
			r.log.InfoLogger.Info("ConsumeMessages received done signal")
			return
		default:
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
				r.log.ErrorLogger.Error("Failed to register a consumer, retrying...", "error", err)
				r.reconnectAndConsume(handler, done)
				continue
			}
			r.consumeLoop(msgs, handler, done)
		}
	}
}

func (r *RabbitMQ) consumeLoop(msgs <-chan amqp.Delivery, handler func(amqp.Delivery), done <-chan struct{}) {
	r.wg.Add(1)
	defer r.wg.Done()

	for {
		select {
		case msg, ok := <-msgs:
			if !ok {
				r.log.InfoLogger.Info("Channel closed")
				r.reconnectAndConsume(handler, done)
				return
			}
			select {
			case <-done:
				r.log.InfoLogger.Info("Message handling received done signal")
				return
			default:
				r.wg.Add(1)
				go func(msg amqp.Delivery) {
					defer r.wg.Done()
					handler(msg)
				}(msg)
			}
		case <-done:
			r.log.InfoLogger.Info("Exiting message loop due to done signal")
			return
		}
	}
}

func (r *RabbitMQ) reconnectAndConsume(handler func(amqp.Delivery), done <-chan struct{}) {
	for {
		select {
		case <-done:
			r.log.InfoLogger.Info("Reconnection stopped due to done signal")
			return
		default:
			err := r.connect()
			if err == nil {
				r.log.InfoLogger.Info("Successfully reconnected to RabbitMQ.")
				go r.ConsumeMessages(handler, done)
				return
			}
			r.log.ErrorLogger.Error("Failed to reconnect to RabbitMQ, retrying...", "error", err)
			time.Sleep(ReconnectDelay)
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
}
