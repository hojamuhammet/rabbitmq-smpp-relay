package smpp

import (
	"time"

	"rabbitmq-smpp-relay/internal/config"
	"rabbitmq-smpp-relay/pkg/logger"

	"github.com/fiorix/go-smpp/smpp"
	"github.com/fiorix/go-smpp/smpp/pdu/pdutext"
)

const (
	RetryDelay     = 5 * time.Second
	SegmentDelay   = 500 * time.Millisecond
	ReconnectDelay = 5 * time.Second
)

type SMPPClient struct {
	Transmitter *smpp.Transmitter
	Logger      *logger.Loggers
}

func NewSMPPClient(cfg *config.Config, loggers *logger.Loggers) (*SMPPClient, error) {
	smppCfg := cfg.SMPP
	tm := &smpp.Transmitter{
		Addr:   smppCfg.Addr,
		User:   smppCfg.User,
		Passwd: smppCfg.Pass,
	}

	connStatus := tm.Bind()
	for status := range connStatus {
		if status.Status() == smpp.Connected {
			loggers.InfoLogger.Info("Connected to SMPP server.")
			break
		} else {
			loggers.ErrorLogger.Error("Failed to connect to SMPP server", "error", status.Error(), "addr", smppCfg.Addr, "user", smppCfg.User)
			time.Sleep(5 * time.Second)
		}
	}

	client := &SMPPClient{Transmitter: tm, Logger: loggers}
	go client.monitorConnection()
	return client, nil
}

func (c *SMPPClient) monitorConnection() {
	for status := range c.Transmitter.Bind() {
		if status.Status() == smpp.Disconnected {
			c.Logger.ErrorLogger.Error("Lost connection to SMPP server", "error", status.Error())
			c.reconnect()
		}
		time.Sleep(1 * time.Second)
	}
}

func (c *SMPPClient) reconnect() {
	for {
		c.Logger.InfoLogger.Info("Attempting to reconnect to SMPP server...")
		connStatus := c.Transmitter.Bind()
		for status := range connStatus {
			if status.Status() == smpp.Connected {
				c.Logger.InfoLogger.Info("Reconnected to SMPP server.")
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
		for {
			pdus, err := c.Transmitter.SubmitLongMsg(shortMsg)
			if err != nil {
				c.Logger.ErrorLogger.Error("Failed to send SMS", "error", err)
				time.Sleep(RetryDelay)
				continue
			}

			for i := range pdus {
				pdu := &pdus[i]
				c.Logger.InfoLogger.Info("Message segment sent successfully", "src", pdu.Src, "dst", pdu.Dst, "text", pdu.Text)
			}

			return
		}
	}()

	return nil
}