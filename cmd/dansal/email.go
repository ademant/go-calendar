package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"mime"
	"net/http"
	"net/smtp"
)

// POST /api/v1/email/test
func sendTestEmail(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	role := r.Header.Get("X-User-Role")
	if role != RoleAdmin {
		http.Error(w, "Forbidden: admin only", http.StatusForbidden)
		return
	}

	var body struct {
		EmailAddress string `json:"emailaddress"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.EmailAddress == "" {
		http.Error(w, "emailaddress is required", http.StatusBadRequest)
		return
	}

	if err := SendEmail(body.EmailAddress, "Dansal SMTP Test", "This is a test email sent by Dansal to verify SMTP configuration."); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	json.NewEncoder(w).Encode(map[string]string{"status": "sent", "to": body.EmailAddress})
}

// smtpObscure encrypts password with AES-256-GCM.
// Pass an existing keyHex to reuse the key; pass "" to generate a new one.
// Returns (base64 ciphertext, hex key).
func smtpObscure(password, keyHex string) (string, string, error) {
	var key []byte
	if keyHex == "" {
		key = make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			return "", "", err
		}
	} else {
		var err error
		key, err = hex.DecodeString(keyHex)
		if err != nil || len(key) != 32 {
			return "", "", fmt.Errorf("invalid password_key")
		}
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", "", err
	}
	ct := gcm.Seal(nonce, nonce, []byte(password), nil)
	return base64.StdEncoding.EncodeToString(ct), hex.EncodeToString(key), nil
}

// smtpReveal decrypts an AES-256-GCM ciphertext produced by smtpObscure.
func smtpReveal(encBase64, keyHex string) (string, error) {
	key, err := hex.DecodeString(keyHex)
	if err != nil || len(key) != 32 {
		return "", fmt.Errorf("invalid password_key")
	}
	ct, err := base64.StdEncoding.DecodeString(encBase64)
	if err != nil {
		return "", fmt.Errorf("base64 decode: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(ct) < gcm.NonceSize() {
		return "", fmt.Errorf("ciphertext too short")
	}
	plain, err := gcm.Open(nil, ct[:gcm.NonceSize()], ct[gcm.NonceSize():], nil)
	if err != nil {
		return "", fmt.Errorf("decrypt failed")
	}
	return string(plain), nil
}

// SendEmail sends a plain-text email using the configured SMTP server.
func SendEmail(to, subject, body string) error {
	cfg := config.SMTP
	if cfg.Host == "" {
		return fmt.Errorf("SMTP not configured")
	}

	port := cfg.Port
	if port == 0 {
		port = 587
	}

	password := ""
	if cfg.Password != "" && cfg.PasswordKey != "" {
		var err error
		password, err = smtpReveal(cfg.Password, cfg.PasswordKey)
		if err != nil {
			return fmt.Errorf("SMTP password: %w", err)
		}
	}

	from := cfg.From
	if from == "" {
		from = cfg.Username
	}
	fromHeader := from
	if cfg.FromName != "" {
		fromHeader = mime.QEncoding.Encode("utf-8", cfg.FromName) + " <" + from + ">"
	}

	msg := []byte("From: " + fromHeader + "\r\n" +
		"To: " + to + "\r\n" +
		"Subject: " + subject + "\r\n" +
		"Content-Type: text/plain; charset=UTF-8\r\n\r\n" +
		body)

	addr := fmt.Sprintf("%s:%d", cfg.Host, port)

	switch cfg.TLS {
	case "tls":
		conn, err := tls.Dial("tcp", addr, &tls.Config{ServerName: cfg.Host})
		if err != nil {
			return fmt.Errorf("TLS dial: %w", err)
		}
		defer conn.Close()
		c, err := smtp.NewClient(conn, cfg.Host)
		if err != nil {
			return fmt.Errorf("SMTP client: %w", err)
		}
		defer c.Quit()
		if cfg.Username != "" {
			if err := c.Auth(smtp.PlainAuth("", cfg.Username, password, cfg.Host)); err != nil {
				return fmt.Errorf("SMTP auth: %w", err)
			}
		}
		if err := c.Mail(from); err != nil {
			return err
		}
		if err := c.Rcpt(to); err != nil {
			return err
		}
		w, err := c.Data()
		if err != nil {
			return err
		}
		if _, err := w.Write(msg); err != nil {
			return err
		}
		return w.Close()
	case "none":
		return smtp.SendMail(addr, nil, from, []string{to}, msg)
	default: // "starttls" or ""
		var auth smtp.Auth
		if cfg.Username != "" {
			auth = smtp.PlainAuth("", cfg.Username, password, cfg.Host)
		}
		return smtp.SendMail(addr, auth, from, []string{to}, msg)
	}
}
