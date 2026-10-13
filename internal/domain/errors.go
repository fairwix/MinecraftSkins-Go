package domain

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
)

var (
	ErrNotFound            = errors.New("resource not found")
	ErrSkinUnavailable     = errors.New("skin is unavailable")
	ErrExternalService     = errors.New("external service unavailable")
	ErrInvalidInput        = errors.New("invalid input")
	ErrIdempotencyConflict = errors.New("idempotency key conflict")
)

func NewID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	raw[6] = raw[6]&0x0f | 0x40
	raw[8] = raw[8]&0x3f | 0x80
	encoded := hex.EncodeToString(raw[:])
	return encoded[:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:], nil
}

type ValidationError struct {
	Fields map[string]string `json:"errors"`
}

func (e *ValidationError) Error() string { return "request validation failed" }

func (e *ValidationError) Is(target error) bool { return target == ErrInvalidInput }

type SkinUnavailableError struct {
	SkinID string `json:"skinId"`
	Reason string `json:"reason"`
}

func (e *SkinUnavailableError) Error() string { return e.Reason }

func (e *SkinUnavailableError) Is(target error) bool { return target == ErrSkinUnavailable }

type ExternalServiceError struct {
	Service string
	Cause   error
}

func (e *ExternalServiceError) Error() string        { return e.Service + ": " + ErrExternalService.Error() }
func (e *ExternalServiceError) Unwrap() error        { return e.Cause }
func (e *ExternalServiceError) Is(target error) bool { return target == ErrExternalService }
