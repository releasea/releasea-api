package auth

import (
	"fmt"
	"net/smtp"
	"net/url"
	"strings"

	"releaseaapi/internal/platform/shared"
)

func deliverPasswordReset(email, token string) error {
	smtpAddress := strings.TrimSpace(shared.EnvOrDefault("PASSWORD_RESET_SMTP_ADDRESS", ""))
	from := strings.TrimSpace(shared.EnvOrDefault("PASSWORD_RESET_SMTP_FROM", ""))
	publicURL := strings.TrimSpace(shared.EnvOrDefault("PASSWORD_RESET_PUBLIC_URL", ""))
	if smtpAddress == "" || from == "" || publicURL == "" {
		return fmt.Errorf("password reset SMTP delivery is not configured")
	}

	resetURL, err := url.Parse(publicURL)
	if err != nil || resetURL.Scheme == "" || resetURL.Host == "" {
		return fmt.Errorf("PASSWORD_RESET_PUBLIC_URL must be an absolute URL")
	}
	query := resetURL.Query()
	query.Set("resetToken", token)
	resetURL.RawQuery = query.Encode()

	host := smtpAddress
	if index := strings.LastIndex(smtpAddress, ":"); index > 0 {
		host = smtpAddress[:index]
	}
	username := strings.TrimSpace(shared.EnvOrDefault("PASSWORD_RESET_SMTP_USERNAME", ""))
	password := shared.EnvOrDefault("PASSWORD_RESET_SMTP_PASSWORD", "")
	var smtpAuth smtp.Auth
	if username != "" {
		smtpAuth = smtp.PlainAuth("", username, password, host)
	}

	message := strings.Join([]string{
		"From: " + from,
		"To: " + email,
		"Subject: Reset your Releasea password",
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
		"",
		"Use this link within 30 minutes to reset your Releasea password:",
		resetURL.String(),
		"",
		"If you did not request this change, you can ignore this email.",
	}, "\r\n")

	return smtp.SendMail(smtpAddress, smtpAuth, from, []string{email}, []byte(message))
}
