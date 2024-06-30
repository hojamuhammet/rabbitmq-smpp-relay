package smpp

import (
	"fmt"
	"rabbitmq-smpp-relay/internal/config"
	"rabbitmq-smpp-relay/pkg/logger"
	"time"

	"github.com/fiorix/go-smpp/smpp"
	"github.com/fiorix/go-smpp/smpp/pdu/pdutext"
)

const (
	MaxRetries   = 3
	RetryDelay   = 2 * time.Second
	SegmentDelay = 500 * time.Millisecond
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
			loggers.ErrorLogger.Error("Failed to connect to SMPP server", "error", status.Error())
			loggers.InfoLogger.Info("Retrying in 5 seconds...")
			time.Sleep(5 * time.Second)
		}
	}

	return &SMPPClient{Transmitter: tm, Logger: loggers}, nil
}

func (c *SMPPClient) SendSMS(src, dest, text string) error {
	c.Logger.InfoLogger.Info("Sending SMS", "src", src, "dst", dest, "text", text)

	shortMsg := &smpp.ShortMessage{
		Src:  src,
		Dst:  dest,
		Text: pdutext.UCS2(text),
	}

	for attempt := 0; attempt < MaxRetries; attempt++ {
		pdus, err := c.Transmitter.SubmitLongMsg(shortMsg)
		if err != nil {
			c.Logger.ErrorLogger.Error("Failed to send SMS", "attempt", attempt+1, "error", err)
			time.Sleep(RetryDelay)
			continue
		}

		for i := range pdus {
			pdu := &pdus[i]
			c.Logger.InfoLogger.Info("Message segment sent successfully", "src", pdu.Src, "dst", pdu.Dst, "text", pdu.Text)
		}

		return nil
	}

	return fmt.Errorf("failed to send SMS after %d attempts", MaxRetries)
}
