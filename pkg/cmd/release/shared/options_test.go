package shared

import (
	"errors"
	"testing"
)

func TestAttestOptions_AreFlagsValid_Valid(t *testing.T) {
	opts := &AttestOptions{
		Repo:  "owner/repo",
		Limit: 10,
	}
	if err := opts.AreFlagsValid(); err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestAttestOptions_AreFlagsValid_InvalidRepo(t *testing.T) {
	opts := &AttestOptions{
		Repo: "invalidrepo",
	}
	err := opts.AreFlagsValid()
	if err == nil || !errors.Is(err, err) {
		t.Errorf("expected error for invalid repo, got %v", err)
	}
}

func TestAttestOptions_AreFlagsValid_LimitTooLow(t *testing.T) {
	opts := &AttestOptions{
		Repo:  "owner/repo",
		Limit: 0,
	}
	err := opts.AreFlagsValid()
	if err == nil || !errors.Is(err, err) {
		t.Errorf("expected error for limit too low, got %v", err)
	}
}

func TestAttestOptions_AreFlagsValid_LimitTooHigh(t *testing.T) {
	opts := &AttestOptions{
		Repo:  "owner/repo",
		Limit: 1001,
	}
	err := opts.AreFlagsValid()
	if err == nil || !errors.Is(err, err) {
		t.Errorf("expected error for limit too high, got %v", err)
	}
}

func TestAttestOptions_AreFlagsValid_ValidHostname(t *testing.T) {
	opts := &AttestOptions{
		Repo:     "owner/repo",
		Limit:    10,
		Hostname: "github.com",
	}
	err := opts.AreFlagsValid()
	if err != nil {
		t.Errorf("expected no error for valid hostname, got %v", err)
	}
}
