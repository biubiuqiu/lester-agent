package secret

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"fmt"
	"io"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	db   *pgxpool.Pool
	aead cipher.AEAD
}

func New(db *pgxpool.Pool, key []byte) (*Store, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Store{db: db, aead: aead}, nil
}
func (s *Store) Put(ctx context.Context, workspaceID uuid.UUID, value []byte) (uuid.UUID, error) {
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return uuid.Nil, err
	}
	ciphertext := s.aead.Seal(nil, nonce, value, []byte(workspaceID.String()))
	var id uuid.UUID
	err := s.db.QueryRow(ctx, `INSERT INTO credentials(workspace_id,ciphertext,nonce) VALUES($1,$2,$3) RETURNING id`, workspaceID, ciphertext, nonce).Scan(&id)
	return id, err
}
func (s *Store) Get(ctx context.Context, workspaceID, id uuid.UUID) ([]byte, error) {
	var ciphertext, nonce []byte
	if err := s.db.QueryRow(ctx, `SELECT ciphertext,nonce FROM credentials WHERE id=$1 AND workspace_id=$2`, id, workspaceID).Scan(&ciphertext, &nonce); err != nil {
		return nil, err
	}
	plain, err := s.aead.Open(nil, nonce, ciphertext, []byte(workspaceID.String()))
	if err != nil {
		return nil, fmt.Errorf("decrypt credential: %w", err)
	}
	return plain, nil
}
