package idgen

import (
	"crypto/rand"

	"github.com/google/uuid"
)

type Generator interface {
	NewUUID() string
	NewToken(n int) ([]byte, error)
}

type DefaultGen struct{}

func (DefaultGen) NewUUID() string { return uuid.NewString() }

func (DefaultGen) NewToken(n int) ([]byte, error) {
	b := make([]byte, n)
	_, err := rand.Read(b)
	return b, err
}
