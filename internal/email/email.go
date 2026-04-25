package email

import (
	"context"
	"fmt"
	"log/slog"
	"os"
)

type Sender interface {
	SendMagicLink(ctx context.Context, email string, link string) error
	SendInvitation(ctx context.Context, email string, link string) error
}

type ConsoleStub struct {
	Logger *slog.Logger
}

func (c ConsoleStub) SendMagicLink(_ context.Context, email string, link string) error {
	return c.write("magic-link", email, link)
}

func (c ConsoleStub) SendInvitation(_ context.Context, email string, link string) error {
	return c.write("invitation", email, link)
}

func (c ConsoleStub) write(kind, email, link string) error {
	if c.Logger != nil {
		c.Logger.Info("email stub", "kind", kind, "email", email, "link", link)
	}
	f, err := os.OpenFile("./dev-emails.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open email log: %w", err)
	}
	defer f.Close()
	if _, err := fmt.Fprintf(f, "%s %s %s\n", kind, email, link); err != nil {
		return fmt.Errorf("write email log: %w", err)
	}
	return nil
}

type ResendAdapter struct {
	APIKey string
	From   string
}

func (r ResendAdapter) SendMagicLink(ctx context.Context, email string, link string) error {
	return ConsoleStub{}.SendMagicLink(ctx, email, link)
}

func (r ResendAdapter) SendInvitation(ctx context.Context, email string, link string) error {
	return ConsoleStub{}.SendInvitation(ctx, email, link)
}
