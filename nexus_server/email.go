package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/smtp"
	"os"
	"strings"
	"time"
)

func (s *NexusServer) sendEmail(to, subject, body string) error {
	// Preference order: Resend (no IP allowlist) → Brevo → SMTP. Each one
	// is opt-in via its env var; users can run with whichever they have.
	if apiKey := os.Getenv("RESEND_API_KEY"); apiKey != "" {
		return sendEmailResend(apiKey, to, subject, body)
	}
	if apiKey := os.Getenv("BREVO_API_KEY"); apiKey != "" {
		return sendEmailBrevo(apiKey, to, subject, body)
	}
	return sendEmailSMTP(to, subject, body)
}

// sendEmailResend posts to Resend's HTTP API. No IP allowlist, no SMTP
// fragility — just an API key. Free tier covers 100 emails/day, plenty
// for verification flows. https://resend.com/docs/api-reference/emails/send-email
func sendEmailResend(apiKey, to, subject, body string) error {
	from := os.Getenv("RESEND_SENDER")
	if from == "" {
		// Resend requires the sender to be from a verified domain. Default
		// to onboarding@resend.dev (Resend's built-in sandbox sender) so
		// the first run works without any DNS setup; users override with
		// RESEND_SENDER="Phaze <noreply@phazechat.world>" once their
		// custom domain is verified in the Resend dashboard.
		from = "Phaze <onboarding@resend.dev>"
	}
	payload := map[string]any{
		"from":    from,
		"to":      []string{to},
		"subject": subject,
		"html":    body,
	}
	buf, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequest("POST", "https://api.resend.com/emails", bytes.NewReader(buf))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	return fmt.Errorf("resend api %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
}

// sendEmailBrevo posts to Brevo's transactional API. Preferred over SMTP — auth
// is a single header, no port 587, and bounces/opens come back via webhooks.
func sendEmailBrevo(apiKey, to, subject, body string) error {
	fromEmail := os.Getenv("BREVO_SENDER_EMAIL")
	if fromEmail == "" {
		fromEmail = os.Getenv("SMTP_FROM")
	}
	if fromEmail == "" {
		fromEmail = "noreply@phazechat.world"
	}
	fromName := os.Getenv("BREVO_SENDER_NAME")
	if fromName == "" {
		fromName = "Phaze"
	}

	payload := map[string]any{
		"sender":      map[string]string{"name": fromName, "email": fromEmail},
		"to":          []map[string]string{{"email": to}},
		"subject":     subject,
		"htmlContent": body,
	}
	buf, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequest("POST", "https://api.brevo.com/v3/smtp/email", bytes.NewReader(buf))
	if err != nil {
		return err
	}
	req.Header.Set("api-key", apiKey)
	req.Header.Set("content-type", "application/json")
	req.Header.Set("accept", "application/json")
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	return fmt.Errorf("brevo api %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
}

func sendEmailSMTP(to, subject, body string) error {
	host := os.Getenv("SMTP_HOST")
	port := os.Getenv("SMTP_PORT")
	user := os.Getenv("SMTP_USER")
	pass := os.Getenv("SMTP_PASS")

	if host == "" || user == "" || pass == "" {
		log.Printf("[MAIL-SIM] To: %s | Subject: %s | Body: %s", to, subject, body)
		return nil
	}

	from := os.Getenv("SMTP_FROM")
	if from == "" {
		from = user
	}
	auth := smtp.PlainAuth("", user, pass, host)
	// C4: sanitize header values — strip CR/LF to prevent SMTP header injection.
	sanitizeHeader := func(s string) string {
		return strings.NewReplacer("\r", "", "\n", "").Replace(s)
	}
	msg := []byte("From: Phaze <" + sanitizeHeader(from) + ">\r\n" +
		"To: " + sanitizeHeader(to) + "\r\n" +
		"Subject: " + sanitizeHeader(subject) + "\r\n" +
		"MIME-version: 1.0\r\nContent-Type: text/html; charset=\"UTF-8\"\r\n" +
		"\r\n" +
		body + "\r\n")

	addr := fmt.Sprintf("%s:%s", host, port)
	if port == "" {
		addr = host + ":587"
	}

	return smtp.SendMail(addr, auth, user, []string{to}, msg)
}

// sendEmailLogged wraps sendEmail for 'go'-launched calls so SMTP failures
// land in logs instead of vanishing silently. Use this any time email
// delivery is not on the caller's synchronous response path.
func (s *NexusServer) sendEmailLogged(to, subject, body string) {
	if err := s.sendEmail(to, subject, body); err != nil {
		log.Printf("[mail] send to %s subject %q failed: %v", to, subject, err)
	}
}
