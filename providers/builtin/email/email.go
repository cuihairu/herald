package email

import (
	"context"
	"fmt"
	"net/smtp"
	"strings"
	"time"

	"github.com/cuihaitao/herald/core"
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
			Name:     "email",
			Type:     "builtin",
			Status:   "available",
			Since:    time.Now(),
		},
	}, nil
}

// Deliver delivers a task via email
func (p *Provider) Deliver(ctx context.Context, task *core.Task) error {
	// Get recipients from task data or target
	to := []string{}
	if task.Target != "" {
		to = append(to, strings.Split(task.Target, ",")...)
	}
	if toData, ok := task.Data["to"]; ok {
		if toSlice, ok := toData.([]string); ok {
			to = append(to, toSlice...)
		} else if toStr, ok := toData.(string); ok {
			to = append(to, strings.Split(toStr, ",")...)
		}
	}

	if len(to) == 0 {
		return fmt.Errorf("email: no recipients specified")
	}

	// Build message
	subject := task.Title
	if task.Level != "" {
		subject = "[" + strings.ToUpper(task.Level) + "] " + subject
	}

	msg := &Message{
		From:    p.formatFrom(),
		To:      to,
		Subject: subject,
		Body:    p.formatBody(task),
		IsHTML:  false,
	}

	return p.sendMessage(ctx, msg)
}

// formatFrom formats the from address
func (p *Provider) formatFrom() string {
	if p.fromName != "" {
		return fmt.Sprintf("%s <%s>", p.fromName, p.from)
	}
	return p.from
}

// formatBody formats the email body
func (p *Provider) formatBody(task *core.Task) string {
	body := ""

	// Add level indicator
	if task.Level != "" {
		body += "Level: " + task.Level + "\n\n"
	}

	// Add title
	body += task.Title + "\n\n"

	// Add separator
	body += strings.Repeat("-", 40) + "\n\n"

	// Add body
	if task.Body != "" {
		body += task.Body + "\n\n"
	}

	// Add timestamp
	body += "Time: " + time.Now().Format("2006-01-02 15:04:05") + "\n"

	return body
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
	return "builtin"
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
	return "builtin"
}
