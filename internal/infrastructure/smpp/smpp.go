package smpp

import (
	"sync"
	"time"

	"rabbitmq-smpp-relay/internal/config"
	"rabbitmq-smpp-relay/pkg/logger"

	"github.com/fiorix/go-smpp/smpp"
	"github.com/fiorix/go-smpp/smpp/pdu/pdutext"
)

const (
	RetryDelay     = 5 * time.Second
	ReconnectDelay = 5 * time.Second
)

type SMPPClient struct {
	Transmitter    *smpp.Transmitter
	Logger         *logger.Loggers
	mu             sync.Mutex
	reconnecting   bool
	isShuttingDown bool
}

func NewSMPPClient(cfg *config.Config, loggers *logger.Loggers) (*SMPPClient, error) {
	smppCfg := cfg.SMPP
	tm := &smpp.Transmitter{
		Addr:   smppCfg.Addr,
		User:   smppCfg.User,
		Passwd: smppCfg.Pass,
	}

	client := &SMPPClient{Transmitter: tm, Logger: loggers}
	client.connect()

	go client.monitorConnection()
	return client, nil
}

func (c *SMPPClient) connect() {
	c.mu.Lock()
	defer c.mu.Unlock()

	connStatus := c.Transmitter.Bind()
	for status := range connStatus {
		if status.Status() == smpp.Connected {
			c.Logger.InfoLogger.Info("Connected to SMPP server.")
			return
		} else {
			c.Logger.ErrorLogger.Error("Failed to connect to SMPP server", "error", status.Error())
			time.Sleep(ReconnectDelay)
		}
	}

	// If connection fails, trigger reconnection
	go c.reconnect()
}

func (c *SMPPClient) monitorConnection() {
	for status := range c.Transmitter.Bind() {
		if status.Status() == smpp.Disconnected && !c.isShuttingDown {
			c.Logger.ErrorLogger.Error("Lost connection to SMPP server", "error", status.Error())
			go c.reconnect() // Use `go` here to avoid blocking the loop
		}
		time.Sleep(1 * time.Second)
	}
}

func (c *SMPPClient) reconnect() {
	c.mu.Lock()
	if c.reconnecting {
		c.mu.Unlock()
		return
	}
	c.reconnecting = true
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		c.reconnecting = false
		c.mu.Unlock()
	}()

	for {
		if c.isShuttingDown {
			return
		}

		c.Logger.InfoLogger.Info("Attempting to reconnect to SMPP server...")
		connStatus := c.Transmitter.Bind()
		for status := range connStatus {
			if status.Status() == smpp.Connected {
				c.Logger.InfoLogger.Info("Reconnected to SMPP server.")
				go c.monitorConnection() // Continue monitoring after successful reconnect
				return
			}
			c.Logger.ErrorLogger.Error("Reconnection failed", "error", status.Error())
		}

		time.Sleep(ReconnectDelay)
	}
}

func (c *SMPPClient) SendSMS(src, dest, text string) error {
	shortMsg := &smpp.ShortMessage{
		Src:  src,
		Dst:  dest,
		Text: pdutext.UCS2(text),
	}

	go func() {
		_, err := c.Transmitter.SubmitLongMsg(shortMsg)
		if err != nil {
			c.Logger.ErrorLogger.Error("Error encountered during SMS submission, triggering SMPP reconnect", "error", err)
			go c.reconnect()
			return
		}

		c.Logger.InfoLogger.Info("Message sent successfully", "src", src, "dst", dest, "text", text)
	}()

	return nil
}

func (c *SMPPClient) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.isShuttingDown = true
	c.Transmitter.Close()
	c.Logger.InfoLogger.Info("SMPP client connection closed")
}
