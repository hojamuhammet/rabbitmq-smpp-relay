package service

import (
	"encoding/json"

	"rabbitmq-smpp-relay/internal/infrastructure/smpp"
	"rabbitmq-smpp-relay/pkg/logger"

	"github.com/streadway/amqp"
)

type Message struct {
	Src string `json:"src"`
	Dst string `json:"dst"`
	Msg string `json:"msg"`
}

type MessageService struct {
	smppClient *smpp.SMPPClient
	loggers    *logger.Loggers
}

func NewMessageService(smppClient *smpp.SMPPClient, loggers *logger.Loggers) *MessageService {
	return &MessageService{
		smppClient: smppClient,
		loggers:    loggers,
	}
}

func (s *MessageService) HandleMessage(msg amqp.Delivery) {
	rawBody := string(msg.Body)
	s.loggers.InfoLogger.Info("Received a raw message", "body", rawBody)

	var message Message
	if err := json.Unmarshal(msg.Body, &message); err != nil {
		s.loggers.ErrorLogger.Error("Invalid message format", "error", err, "body", rawBody)
		return
	}

	if message.Src == "" || message.Dst == "" || message.Msg == "" {
		s.loggers.ErrorLogger.Error("Message fields cannot be empty", "src", message.Src, "dst", message.Dst, "msg", message.Msg)
		return
	}

	if err := s.smppClient.SendSMS(message.Src, message.Dst, message.Msg); err != nil {
		s.loggers.ErrorLogger.Error("Failed to send SMS", "error", err)
	}
}
