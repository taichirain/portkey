package consumer

import (
	"strings"
	"testing"
)

func TestNew_ValidUsername(t *testing.T) {
	c, err := New("alice", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.Username != "alice" {
		t.Errorf("expected username 'alice', got '%s'", c.Username)
	}
	if c.CustomID != "" {
		t.Errorf("expected empty custom_id, got '%s'", c.CustomID)
	}
}

func TestNew_ValidCustomID(t *testing.T) {
	c, err := New("", "ext-123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.CustomID != "ext-123" {
		t.Errorf("expected custom_id 'ext-123', got '%s'", c.CustomID)
	}
}

func TestNew_BothEmpty(t *testing.T) {
	_, err := New("", "")
	if err != ErrConsumerUsernameOrCustomIDRequired {
		t.Errorf("expected ErrConsumerUsernameOrCustomIDRequired, got %v", err)
	}
}

func TestNew_WhitespaceOnly(t *testing.T) {
	_, err := New("   ", "  ")
	if err != ErrConsumerUsernameOrCustomIDRequired {
		t.Errorf("expected ErrConsumerUsernameOrCustomIDRequired, got %v", err)
	}
}

func TestNew_TrimsWhitespace(t *testing.T) {
	c, err := New("  alice  ", "  ext-1  ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.Username != "alice" {
		t.Errorf("expected 'alice', got '%s'", c.Username)
	}
	if c.CustomID != "ext-1" {
		t.Errorf("expected 'ext-1', got '%s'", c.CustomID)
	}
}

func TestValidate_Valid(t *testing.T) {
	c, _ := New("alice", "")
	if err := c.Validate(); err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestValidate_BothEmpty(t *testing.T) {
	c, _ := New("alice", "")
	c.Username = ""
	if err := c.Validate(); err != ErrConsumerUsernameOrCustomIDRequired {
		t.Errorf("expected ErrConsumerUsernameOrCustomIDRequired, got %v", err)
	}
}

func TestValidate_UsernameTooLong(t *testing.T) {
	c, _ := New("alice", "")
	c.Username = strings.Repeat("a", 256)
	if err := c.Validate(); err != ErrConsumerUsernameTooLong {
		t.Errorf("expected ErrConsumerUsernameTooLong, got %v", err)
	}
}

func TestValidate_CustomIDTooLong(t *testing.T) {
	c, _ := New("", "ext")
	c.CustomID = strings.Repeat("b", 256)
	if err := c.Validate(); err != ErrConsumerCustomIDTooLong {
		t.Errorf("expected ErrConsumerCustomIDTooLong, got %v", err)
	}
}

func TestAddTag_NoDuplicate(t *testing.T) {
	c, _ := New("alice", "")
	c.AddTag("premium")
	c.AddTag("beta")
	c.AddTag("premium")
	if len(c.Tags) != 2 {
		t.Errorf("expected 2 tags, got %d: %v", len(c.Tags), c.Tags)
	}
}

func TestRemoveTag(t *testing.T) {
	c, _ := New("alice", "")
	c.AddTag("premium")
	c.AddTag("beta")
	c.RemoveTag("premium")
	if len(c.Tags) != 1 || c.Tags[0] != "beta" {
		t.Errorf("expected [beta], got %v", c.Tags)
	}
}

func TestRemoveTag_NotPresent(t *testing.T) {
	c, _ := New("alice", "")
	c.AddTag("premium")
	c.RemoveTag("nonexistent")
	if len(c.Tags) != 1 {
		t.Errorf("expected 1 tag, got %d", len(c.Tags))
	}
}
