package notifications

import (
	"bytes"
	"dhi-oss-usage/internal/db"
	"encoding/json"
	"fmt"
	"net/http"
	"net/smtp"
	"strings"
	"time"
)

// Provider interface for different notification types
type Provider interface {
	Send(message Message) error
	Type() string
}

// Message represents a notification message
type Message struct {
	Subject string
	Body    string
	Project *db.Project
}

// Service handles sending notifications
type Service struct {
	db *db.DB
}

func NewService(database *db.DB) *Service {
	return &Service{db: database}
}

// NotifyNewProjects sends notifications about new projects to all enabled configs
func (s *Service) NotifyNewProjects(projects []db.Project) error {
	if len(projects) == 0 {
		return nil
	}

	configs, err := s.db.GetEnabledNotificationConfigs()
	if err != nil {
		return fmt.Errorf("getting enabled notification configs: %w", err)
	}

	for _, config := range configs {
		provider, err := s.createProvider(&config)
		if err != nil {
			// Log error but continue with other configs
			s.logNotification(config.ID, nil, "failed", fmt.Sprintf("failed to create provider: %v", err))
			continue
		}

		// Send notification for each new project
		for _, project := range projects {
			message := s.buildNewProjectMessage(&project)
			err := provider.Send(message)
			
			projectID := project.ID
			if err != nil {
				s.logNotification(config.ID, &projectID, "failed", err.Error())
			} else {
				s.logNotification(config.ID, &projectID, "sent", "")
			}
		}

		// Update last triggered time
		s.db.UpdateNotificationTriggered(config.ID)
	}

	return nil
}

// SendTestNotification sends a test notification for a specific config
func (s *Service) SendTestNotification(configID int64) error {
	config, err := s.db.GetNotificationConfig(configID)
	if err != nil {
		return fmt.Errorf("getting notification config: %w", err)
	}
	if config == nil {
		return fmt.Errorf("notification config not found")
	}

	provider, err := s.createProvider(config)
	if err != nil {
		return fmt.Errorf("creating provider: %w", err)
	}

	message := Message{
		Subject: "DHI OSS Tracker - Test Notification",
		Body:    fmt.Sprintf("This is a test notification from DHI OSS Tracker.\n\nNotification: %s\nType: %s\nTime: %s", config.Name, config.Type, time.Now().Format(time.RFC1123)),
	}

	err = provider.Send(message)
	if err != nil {
		s.logNotification(configID, nil, "failed", err.Error())
		return err
	}

	s.logNotification(configID, nil, "sent", "")
	return nil
}

func (s *Service) createProvider(config *db.NotificationConfig) (Provider, error) {
	switch config.Type {
	case "slack":
		return newSlackProvider(config.ConfigJSON)
	case "email":
		return newEmailProvider(config.ConfigJSON)
	default:
		return nil, fmt.Errorf("unknown notification type: %s", config.Type)
	}
}

func (s *Service) buildNewProjectMessage(project *db.Project) Message {
	body := fmt.Sprintf(
		"New DHI Adoption Detected!\n\n"+
			"Repository: %s\n"+
			"Stars: %d ⭐\n"+
			"Description: %s\n"+
			"GitHub: %s\n"+
			"Source: %s\n",
		project.RepoFullName,
		project.Stars,
		project.Description,
		project.GitHubURL,
		project.SourceType,
	)

	if project.AdoptedAt != nil {
		body += fmt.Sprintf("Adopted: %s\n", project.AdoptedAt.Format("2006-01-02"))
	}
	if project.AdoptionCommit != "" {
		body += fmt.Sprintf("Commit: %s\n", project.AdoptionCommit)
	}

	return Message{
		Subject: fmt.Sprintf("New DHI Adoption: %s (%d⭐)", project.RepoFullName, project.Stars),
		Body:    body,
		Project: project,
	}
}

func (s *Service) logNotification(configID int64, projectID *int64, status string, errorMsg string) {
	log := &db.NotificationLog{
		ConfigID:     configID,
		ProjectID:    projectID,
		Status:       status,
		ErrorMessage: errorMsg,
	}
	s.db.CreateNotificationLog(log)
}

// Slack Provider

type SlackConfig struct {
	WebhookURL string `json:"webhook_url"`
	Channel    string `json:"channel,omitempty"`
}

type slackProvider struct {
	config SlackConfig
}

func newSlackProvider(configJSON string) (*slackProvider, error) {
	var config SlackConfig
	if err := json.Unmarshal([]byte(configJSON), &config); err != nil {
		return nil, fmt.Errorf("parsing slack config: %w", err)
	}
	if config.WebhookURL == "" {
		return nil, fmt.Errorf("webhook_url is required")
	}
	return &slackProvider{config: config}, nil
}

func (p *slackProvider) Type() string {
	return "slack"
}

func (p *slackProvider) Send(msg Message) error {
	// Build Slack message with blocks for better formatting
	blocks := []map[string]interface{}{
		{
			"type": "header",
			"text": map[string]string{
				"type": "plain_text",
				"text": "🐳 New DHI Adoption",
			},
		},
	}

	if msg.Project != nil {
		// Project notification
		fields := []map[string]interface{}{
			{
				"type": "mrkdwn",
				"text": fmt.Sprintf("*Repository:*\n<%s|%s>", msg.Project.GitHubURL, msg.Project.RepoFullName),
			},
			{
				"type": "mrkdwn",
				"text": fmt.Sprintf("*Stars:*\n%d ⭐", msg.Project.Stars),
			},
		}

		if msg.Project.SourceType != "" {
			fields = append(fields, map[string]interface{}{
				"type": "mrkdwn",
				"text": fmt.Sprintf("*Source:*\n%s", msg.Project.SourceType),
			})
		}

		blocks = append(blocks, map[string]interface{}{
			"type":   "section",
			"fields": fields,
		})

		if msg.Project.Description != "" {
			blocks = append(blocks, map[string]interface{}{
				"type": "section",
				"text": map[string]string{
					"type": "mrkdwn",
					"text": fmt.Sprintf("*Description:*\n%s", msg.Project.Description),
				},
			})
		}

		if msg.Project.AdoptionCommit != "" {
			blocks = append(blocks, map[string]interface{}{
				"type": "section",
				"text": map[string]string{
					"type": "mrkdwn",
					"text": fmt.Sprintf("<%s|View Adoption Commit>", msg.Project.AdoptionCommit),
				},
			})
		}
	} else {
		// Test notification
		blocks = append(blocks, map[string]interface{}{
			"type": "section",
			"text": map[string]string{
				"type": "mrkdwn",
				"text": msg.Body,
			},
		})
	}

	payload := map[string]interface{}{
		"blocks": blocks,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshaling slack payload: %w", err)
	}

	resp, err := http.Post(p.config.WebhookURL, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("sending slack webhook: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("slack webhook returned status %d", resp.StatusCode)
	}

	return nil
}

// Email Provider

type EmailConfig struct {
	To           string `json:"to"`
	SMTPHost     string `json:"smtp_host"`
	SMTPPort     int    `json:"smtp_port"`
	SMTPUsername string `json:"smtp_username"`
	SMTPPassword string `json:"smtp_password"`
	From         string `json:"from"`
}

type emailProvider struct {
	config EmailConfig
}

func newEmailProvider(configJSON string) (*emailProvider, error) {
	var config EmailConfig
	if err := json.Unmarshal([]byte(configJSON), &config); err != nil {
		return nil, fmt.Errorf("parsing email config: %w", err)
	}
	if config.To == "" || config.SMTPHost == "" || config.SMTPPort == 0 || config.From == "" {
		return nil, fmt.Errorf("missing required email config fields")
	}
	return &emailProvider{config: config}, nil
}

func (p *emailProvider) Type() string {
	return "email"
}

func (p *emailProvider) Send(msg Message) error {
	// Build email
	subject := msg.Subject
	body := msg.Body

	headers := make(map[string]string)
	headers["From"] = p.config.From
	headers["To"] = p.config.To
	headers["Subject"] = subject
	headers["MIME-Version"] = "1.0"
	headers["Content-Type"] = "text/plain; charset=\"utf-8\""

	var emailMsg strings.Builder
	for k, v := range headers {
		emailMsg.WriteString(fmt.Sprintf("%s: %s\r\n", k, v))
	}
	emailMsg.WriteString("\r\n")
	emailMsg.WriteString(body)

	// Send email
	addr := fmt.Sprintf("%s:%d", p.config.SMTPHost, p.config.SMTPPort)
	
	var auth smtp.Auth
	if p.config.SMTPUsername != "" && p.config.SMTPPassword != "" {
		auth = smtp.PlainAuth("", p.config.SMTPUsername, p.config.SMTPPassword, p.config.SMTPHost)
	}

	err := smtp.SendMail(addr, auth, p.config.From, []string{p.config.To}, []byte(emailMsg.String()))
	if err != nil {
		return fmt.Errorf("sending email: %w", err)
	}

	return nil
}
