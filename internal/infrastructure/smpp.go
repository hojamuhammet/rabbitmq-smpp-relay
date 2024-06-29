package smpp

import (
	"fmt"
	"log"
	"rabbitmq-smpp-relay/internal/config"
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
}

func NewSMPPClient(cfg *config.Config) (*SMPPClient, error) {
	smppCfg := cfg.SMPP
	tm := &smpp.Transmitter{
		Addr:   smppCfg.Addr,
		User:   smppCfg.User,
		Passwd: smppCfg.Pass,
	}

	connStatus := tm.Bind()
	for status := range connStatus {
		if status.Status() == smpp.Connected {
			log.Println("Connected to SMPP server.")
			break
		} else {
			log.Println("Failed to connect to SMPP server:", status.Error())
			log.Println("Retrying in 5 seconds...")
			time.Sleep(5 * time.Second)
		}
	}

	return &SMPPClient{Transmitter: tm}, nil
}

func (c *SMPPClient) SendSMS(src, dest, text string) error {
	shortMsg := &smpp.ShortMessage{
		Src:  src,
		Dst:  dest,
		Text: pdutext.UCS2(text), // Automatically handles UCS2 encoding
	}

	for attempt := 0; attempt < MaxRetries; attempt++ {
		pdus, err := c.Transmitter.SubmitLongMsg(shortMsg)
		if err != nil {
			log.Printf("Failed to send SMS, attempt %d, error: %v", attempt+1, err)
			time.Sleep(RetryDelay)
			continue
		}

		for i := range pdus {
			pdu := &pdus[i]
			log.Printf("Message segment sent successfully, Src: %s, Dst: %s, Text: %s", pdu.Src, pdu.Dst, pdu.Text)
		}

		return nil
	}

	return fmt.Errorf("failed to send SMS after %d attempts", MaxRetries)
}
