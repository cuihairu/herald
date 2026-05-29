package email

import (
	"context"
	"fmt"
	"net/smtp"
	"strings"
	"time"

	"github.com/cuihairu/herald/core"
)

// Provider is an email provider
type Provider struct {
	host     string
	port     int
	username string
	password string
	from     string
	fromName string
	status   *core.ProviderStatus
}

// Config is the email provider configuration
type Config struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
	From     string `yaml:"from"`
	FromName string `yaml:"from_name"`
}

// Message is an email message
type Message struct {
	From    string
	To      []string
	Subject string
	Body    string
	IsHTML  bool
}

// NewProvider creates a new email provider
func NewProvider(config map[string]interface{}) (core.Provider, error) {
	host, ok := config["host"].(string)
	if !ok || host == "" {
		return nil, fmt.Errorf("email: host is required")
	}

	var port int
	if p, ok := config["port"].(int); ok {
		port = p
	} else if p, ok := config["port"].(float64); ok {
		port = int(p)
	} else {
		port = 587 // Default SMTP port
	}

	username, _ := config["username"].(string)
	password, _ := config["password"].(string)
	from, _ := config["from"].(string)
	fromName, _ := config["from_name"].(string)

	// Default from address
	if from == "" && username != "" {
		from = username
	}
	if from == "" {
		return nil, fmt.Errorf("email: from is required")
	}

	return &Provider{
		host:     host,
		port:     port,
		username: username,
		password: password,
		from:     from,
		fromName: fromName,
		status: &core.ProviderStatus{
			Name:   "email",
			Type:   "email",
			Status: "available",
			Since:  time.Now(),
		},
	}, nil
}

// GetConfig returns the provider configuration
func (p *Provider) GetConfig() map[string]interface{} {
	return map[string]interface{}{
		"host":      p.host,
		"port":      p.port,
		"username":  p.username,
		"password":  p.password,
		"from":      p.from,
		"from_name": p.fromName,
	}
}

// Capability returns the provider capabilities
func (p *Provider) Capability() core.ProviderCapability {
	return core.ProviderCapability{
		PayloadKinds:   []core.PayloadKind{core.PayloadContent},
		ContentFormats: []string{"html", "plain"},
	}
}

// Deliver delivers a task via email
func (p *Provider) Deliver(ctx context.Context, task *core.DeliveryTask) error {
	// Get recipients from targets
	to := task.Targets

	if len(to) == 0 {
		return fmt.Errorf("email: no recipients specified")
	}

	// Extract content
	title, body := extractContent(task)

	// Build message
	subject := title
	if task.Level != "" {
		subject = "[" + strings.ToUpper(task.Level) + "] " + subject
	}

	// Determine if HTML based on content format
	isHTML := false
	if task.Payload.Content != nil {
		isHTML = task.Payload.Content.Format == "html"
	}

	msg := &Message{
		From:    p.formatFrom(),
		To:      to,
		Subject: subject,
		Body:    body,
		IsHTML:  isHTML,
	}

	return p.sendMessage(ctx, msg)
}

// extractContent extracts title and body from a DeliveryTask
func extractContent(task *core.DeliveryTask) (title, body string) {
	if task.Payload.Content != nil {
		return task.Payload.Content.Title, task.Payload.Content.Body
	}
	return "", ""
}

// formatFrom formats the from address
func (p *Provider) formatFrom() string {
	if p.fromName != "" {
		return fmt.Sprintf("%s <%s>", p.fromName, p.from)
	}
	return p.from
}

// sendMessage sends an email
func (p *Provider) sendMessage(ctx context.Context, msg *Message) error {
	// Build email content
	content := fmt.Sprintf("Subject: %s\r\n", msg.Subject)
	content += fmt.Sprintf("From: %s\r\n", msg.From)
	content += fmt.Sprintf("To: %s\r\n", strings.Join(msg.To, ", "))
	content += "MIME-Version: 1.0\r\n"

	if msg.IsHTML {
		content += "Content-Type: text/html; charset=UTF-8\r\n"
	} else {
		content += "Content-Type: text/plain; charset=UTF-8\r\n"
	}

	content += "\r\n"
	content += msg.Body

	// Send email
	addr := fmt.Sprintf("%s:%d", p.host, p.port)

	var auth smtp.Auth
	if p.username != "" && p.password != "" {
		auth = smtp.PlainAuth("", p.username, p.password, p.host)
	}

	if err := smtp.SendMail(addr, auth, p.from, msg.To, []byte(content)); err != nil {
		return fmt.Errorf("failed to send email: %w", err)
	}

	return nil
}

// Name returns the provider name
func (p *Provider) Name() string {
	return "email"
}

// Type returns the provider type
func (p *Provider) Type() string {
	return "email"
}

// Status returns the current status
func (p *Provider) Status() *core.ProviderStatus {
	p.status.Status = "available"
	return p.status
}

// Close closes the provider
func (p *Provider) Close() error {
	return nil
}

// Factory creates email providers
type Factory struct{}

// Create creates a new email provider
func (f *Factory) Create(config map[string]interface{}) (core.Provider, error) {
	return NewProvider(config)
}

// Name returns the factory name
func (f *Factory) Name() string {
	return "email"
}

// Type returns the factory type
func (f *Factory) Type() string {
	return "email"
}
